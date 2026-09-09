package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"yoyaku_mate_server/utils"
)

// 点主アプリの自動翻訳機能 (メニュー名/カテゴリー名/待機メモ) のGemini APIプロキシ。
// APIキーはサーバー側の環境変数からのみ取得し、クライアントには一切露出しない。
// Gemini呼び出し自体の構造体 (GeminiRequest等) はai_chat_handler.goのものを再利用する。

var errGeminiRateLimited = errors.New("gemini API rate limited")

// callGeminiForText Gemini APIを呼び出し、生成されたテキスト部分のみを返す共通ヘルパー
func callGeminiForText(prompt string) (string, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}

	geminiReq := GeminiRequest{
		Contents: []GeminiContent{
			{Role: "user", Parts: []GeminiPart{{Text: prompt}}},
		},
	}
	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return "", fmt.Errorf("failed to build AI request: %w", err)
	}

	geminiURL := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=%s",
		apiKey,
	)

	resp, err := http.Post(geminiURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to reach AI service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", errGeminiRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gemini API returned status %d: %s", resp.StatusCode, string(body))
	}

	var geminiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return "", fmt.Errorf("failed to parse AI response: %w", err)
	}

	if len(geminiResp.Candidates) > 0 {
		// thinking モデルは parts[0] に thought パートが来る場合があるため、
		// thought フラグが false の最初のテキストパートを実際の返答として使用する
		for _, part := range geminiResp.Candidates[0].Content.Parts {
			if !part.Thought && part.Text != "" {
				return part.Text, nil
			}
		}
	}
	// テキストパートが無い(候補が空/全て thought)場合はエラーではなく空文字を返す。
	// 呼び出し元が用途に応じたフォールバック(空翻訳 / 定型のお詫びメッセージなど)を判断する
	return "", nil
}

// respondGeminiError callGeminiForTextのエラーをHTTPレスポンスに変換する共通処理
func respondGeminiError(w http.ResponseWriter, context string, err error) {
	if errors.Is(err, errGeminiRateLimited) {
		utils.RespondWithError(w, "Translation service is busy. Please try again later.", http.StatusTooManyRequests)
		return
	}
	log.Printf("[%s] %v", context, err)
	utils.RespondWithError(w, "Translation failed", http.StatusBadGateway)
}

// normalizeLanguageCode Geminiが返す言語表記(コード/英語名など揺れがある)を短縮ISOコードに正規化する。
// Flutter側 TranslationService.normalizeLanguageCode と同じ判定ロジック。
func normalizeLanguageCode(code string) string {
	lower := strings.ToLower(code)
	switch {
	case strings.Contains(lower, "english") || lower == "en-us" || lower == "en":
		return "en"
	case strings.Contains(lower, "korean") || lower == "ko":
		return "ko"
	case strings.Contains(lower, "traditional chinese") || strings.Contains(lower, "hant") || lower == "zh-tw":
		return "zh-TW"
	case strings.Contains(lower, "chinese") || strings.Contains(lower, "hans") || lower == "zh-cn" || lower == "zh":
		return "zh"
	case strings.Contains(lower, "japanese") || lower == "ja":
		return "ja"
	case strings.Contains(lower, "spanish") || lower == "es":
		return "es"
	case strings.Contains(lower, "french") || lower == "fr":
		return "fr"
	case strings.Contains(lower, "german") || lower == "de":
		return "de"
	case strings.Contains(lower, "italian") || lower == "it":
		return "it"
	case strings.Contains(lower, "arabic") || lower == "ar":
		return "ar"
	case strings.Contains(lower, "russian") || lower == "ru":
		return "ru"
	}
	return code
}

// ==========================================================
// 単一テキスト翻訳 (待機メモの翻訳表示などで使用)
// ==========================================================

// HandleTranslate POST /api/provider_translate
func HandleTranslate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Text       string `json:"text"`
		TargetLang string `json:"target_lang"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.Text == "" {
		utils.RespondWithError(w, "text is required", http.StatusBadRequest)
		return
	}
	targetLang := req.TargetLang
	if targetLang == "" {
		targetLang = "Japanese"
	}

	prompt := fmt.Sprintf(
		"Translate the following text to %s naturally. Only return the translated text, no explanations:\n\n%s",
		targetLang, req.Text,
	)

	translated, err := callGeminiForText(prompt)
	if err != nil {
		respondGeminiError(w, "HandleTranslate", err)
		return
	}

	utils.RespondWithJSON(w, map[string]interface{}{
		"translated_text": translated,
	}, http.StatusOK)
}

// ==========================================================
// 多言語一括翻訳 (メニュー名/説明/カテゴリー名の自動翻訳で使用)
// ==========================================================

var geminiLanguageNames = map[string]string{
	"en":    "English",
	"ko":    "Korean",
	"zh":    "Chinese (Simplified)",
	"zh-TW": "Traditional Chinese (Taiwan)",
	"es":    "Spanish",
	"fr":    "French",
	"de":    "German",
	"it":    "Italian",
	"ar":    "Arabic",
	"ru":    "Russian",
}

// HandleTranslateMulti POST /api/provider_translate/multi
func HandleTranslateMulti(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Texts           map[string]string `json:"texts"`
		TargetLanguages []string          `json:"target_languages"`
		SmartMenuMode   bool              `json:"smart_menu_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(req.Texts) == 0 || len(req.TargetLanguages) == 0 {
		utils.RespondWithJSON(w, map[string]interface{}{"translations": map[string]interface{}{}}, http.StatusOK)
		return
	}

	prompt := buildMultiTranslatePrompt(req.Texts, req.TargetLanguages, req.SmartMenuMode)

	responseText, err := callGeminiForText(prompt)
	if err != nil {
		respondGeminiError(w, "HandleTranslateMulti", err)
		return
	}

	// マークダウンのコードブロック記法が付いていれば取り除く
	cleanJSON := strings.TrimSpace(responseText)
	cleanJSON = strings.TrimPrefix(cleanJSON, "```json")
	cleanJSON = strings.TrimPrefix(cleanJSON, "```")
	cleanJSON = strings.TrimSuffix(cleanJSON, "```")

	var deepMap map[string]map[string]string
	if err := json.Unmarshal([]byte(cleanJSON), &deepMap); err != nil {
		log.Printf("[HandleTranslateMulti] Failed to parse Gemini JSON response: %v (raw: %s)", err, cleanJSON)
		utils.RespondWithError(w, "Failed to parse translation result", http.StatusBadGateway)
		return
	}

	// 要求した言語のみを残し、キーをISOコードに正規化する
	requested := make(map[string]bool, len(req.TargetLanguages))
	for _, lang := range req.TargetLanguages {
		requested[lang] = true
	}
	translations := make(map[string]map[string]string, len(deepMap))
	for lang, transMap := range deepMap {
		normalized := normalizeLanguageCode(lang)
		if requested[normalized] {
			translations[normalized] = transMap
		}
	}

	utils.RespondWithJSON(w, map[string]interface{}{
		"translations": translations,
	}, http.StatusOK)
}

// buildMultiTranslatePrompt 多言語一括翻訳用のプロンプトを組み立てる。
// ルールはsmartMenuModeの有無に関わらず番号が1から連番になるよう動的に構築する。
func buildMultiTranslatePrompt(texts map[string]string, targetLanguages []string, smartMenuMode bool) string {
	promptLangNames := make([]string, 0, len(targetLanguages))
	for _, code := range targetLanguages {
		if name, ok := geminiLanguageNames[code]; ok {
			promptLangNames = append(promptLangNames, name)
		} else {
			promptLangNames = append(promptLangNames, code)
		}
	}
	promptLanguages := strings.Join(promptLangNames, ", ")

	lines := make([]string, 0, len(texts))
	for id, text := range texts {
		lines = append(lines, fmt.Sprintf("%s: %s", id, text))
	}
	promptLines := strings.Join(lines, "\n")

	rules := []string{
		"Translate naturally and accurately for a restaurant menu.",
	}
	if smartMenuMode {
		rules = append(rules,
			"If the input Key starts with \"t_\" (Title), you MUST append the Japanese pronunciation in Romaji separated by \" / \". \n"+
				"   Format: \"Translated Text / Romaji\". Example: \"Fried Chicken / Karaage\".",
			"If the input Key starts with \"d_\" (Description), do NOT append Romaji.",
		)
	}
	rules = append(rules, "Return ONLY the JSON object. No markdown formatting, no code blocks, no intro text.")

	rulesLines := make([]string, len(rules))
	for i, rule := range rules {
		rulesLines[i] = fmt.Sprintf("%d. %s", i+1, rule)
	}
	rulesText := strings.Join(rulesLines, "\n")

	return fmt.Sprintf(`You are a professional menu translator. Translate the following lines into the following languages: %s.
The input lines are in "id: text" format.
Return a SINGLE JSON object where:
- Keys are the EXACT Language Codes provided: %v.
- For Chinese, use "zh" for Simplified and "zh-TW" for Traditional Chinese.
- Values are objects mapping "id" to "translated_text".
Rules:
%s

Input Data:
%s
`, promptLanguages, targetLanguages, rulesText, promptLines)
}
