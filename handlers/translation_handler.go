package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"yoyaku_mate_server/utils"
)

// 点主アプリの自動翻訳機能 (メニュー名/カテゴリー名/待機メモ) のGemini APIプロキシ。
// APIキーはサーバー側の環境変数からのみ取得し、クライアントには一切露出しない。
// Gemini呼び出し自体の構造体 (GeminiRequest等) はai_chat_handler.goのものを再利用する。

var errGeminiRateLimited = errors.New("gemini API rate limited")

// geminiTimeout Gemini API 1回の呼び出しに許す上限時間。
//
//   - 以前は http.Post (= http.DefaultClient) を使っており、タイムアウトが「無制限」だった。
//     Geminiが応答を返さないと、そのリクエストはゴルーチンとfly.ioプロキシの接続枠を
//     永久に掴んだままになる。呼び出し元の1つ /api/public/ai-chat は無認証の公開
//     エンドポイントであり、マシン1台・256MB構成ではこれがサービス全体の停止に直結する
//   - 両方の呼び出し経路とも ThinkingBudget: 0 (推論無効) のため通常は数秒で返る。
//     15秒は「遅いが正常」を切り捨てない範囲での上限
const geminiTimeout = 15 * time.Second

// geminiErrorBodyLimit エラー応答の本文をログ用に読み取る上限。
// 原因の特定にはこれだけあれば足り、巨大な応答でメモリを踏まれることもない
const geminiErrorBodyLimit = 8 << 10 // 8KiB

// geminiClient Gemini API専用のHTTPクライアント。
// パッケージレベルで1つだけ持ち、コネクションプールを使い回す
// (リクエストごとに生成すると毎回TCP+TLSハンドシェイクからやり直しになる)
var geminiClient = &http.Client{Timeout: geminiTimeout}

// callGeminiForText Gemini APIを呼び出し、生成されたテキスト部分のみを返す共通ヘルパー。
//
// ctx には呼び出し元のリクエストコンテキスト (r.Context()) を渡すこと。
// 客がチャット画面を閉じた時点でGemini呼び出しも中断され、無駄な待ちと課金が発生しない
func callGeminiForText(ctx context.Context, prompt string) (string, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}

	geminiReq := GeminiRequest{
		Contents: []GeminiContent{
			{Role: "user", Parts: []GeminiPart{{Text: prompt}}},
		},
		// 翻訳は推論不要な機械的タスクのため、thinkingを無効化して即時応答させる。
		// 有効のままだと、thinking予算を使い切って最終テキストを一切返さないまま
		// 応答が返る(結果的に空の翻訳になる)ケースがあった
		GenerationConfig: &GenerationConfig{
			ThinkingConfig: &ThinkingConfig{ThinkingBudget: 0},
		},
	}
	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return "", fmt.Errorf("failed to build AI request: %w", err)
	}

	// - APIキーはクエリパラメータ(?key=)ではなくヘッダで渡す。
	//   net/httpの通信エラーは *url.Error として「URL全体」をメッセージに含むため、
	//   クエリに載せるとタイムアウト1回ごとにAPIキーがログへ平文で流れ出る
	const geminiURL = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, geminiURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to build AI request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := geminiClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to reach AI service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", errGeminiRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, geminiErrorBodyLimit))
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

	translated, err := callGeminiForText(r.Context(), prompt)
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
	// メニュー入力者が日本語話者とは限らない(外国人スタッフの可能性がある)ため、
	// 日本語もクライアント側から翻訳対象言語として送られてくることがある
	"ja":    "Japanese",
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

	responseText, err := callGeminiForText(r.Context(), prompt)
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
			"If the input Key starts with \"c_\" (Category name), do NOT append Romaji.",
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
