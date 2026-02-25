package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"yoyaku_mate_server/utils"
)

// AIChatRequest フロントエンドから受け取るリクエスト構造体
type AIChatRequest struct {
	UserMessage  string `json:"userMessage"`
	SystemPrompt string `json:"systemPrompt"`
}

// AIChatResponse フロントエンドへ返すレスポンス構造体
type AIChatResponse struct {
	Reply string `json:"reply"`
}

// GeminiRequest Gemini APIへ送るリクエスト構造体
type GeminiRequest struct {
	Contents []GeminiContent `json:"contents"`
}

type GeminiContent struct {
	Role  string       `json:"role"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text    string `json:"text"`
	Thought bool   `json:"thought"` // gemini-2.5系の「思考」パートを判別するフィールド
}

// GeminiResponse Gemini APIから受け取るレスポンス構造体
type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []GeminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// AIChatHandler Gemini APIのプロキシエンドポイント
// POST /api/public/ai-chat
func AIChatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// リクエストボディのパース
	var req AIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.UserMessage == "" {
		utils.RespondWithError(w, "userMessage is required", http.StatusBadRequest)
		return
	}

	// APIキーをサーバーサイドの環境変数から取得 (ブラウザには絶対に露出しない)
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Println("[AIChatHandler] ERROR: GEMINI_API_KEY environment variable is not set")
		utils.RespondWithError(w, "AI service is not configured", http.StatusInternalServerError)
		return
	}

	// Gemini APIへ送るリクエストを構築
	fullPrompt := req.SystemPrompt + "\n\nお客様: " + req.UserMessage
	geminiReq := GeminiRequest{
		Contents: []GeminiContent{
			{
				Role:  "user",
				Parts: []GeminiPart{{Text: fullPrompt}},
			},
		},
	}

	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		utils.RespondWithError(w, "Failed to build AI request", http.StatusInternalServerError)
		return
	}

	// Gemini API呼び出し
	geminiURL := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=%s",
		apiKey,
	)

	resp, err := http.Post(geminiURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		log.Printf("[AIChatHandler] Gemini API request failed: %v", err)
		utils.RespondWithError(w, "Failed to reach AI service", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Gemini APIのレート制限エラーを処理
	if resp.StatusCode == http.StatusTooManyRequests {
		utils.RespondWithError(w, "AI service is busy. Please try again later.", http.StatusTooManyRequests)
		return
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[AIChatHandler] Gemini API returned status %d: %s", resp.StatusCode, string(body))
		utils.RespondWithError(w, "AI service error", http.StatusBadGateway)
		return
	}

	// レスポンスのパースと返却
	var geminiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		utils.RespondWithError(w, "Failed to parse AI response", http.StatusInternalServerError)
		return
	}

	replyText := "すみません、うまく聞き取れませんでした。"
	if len(geminiResp.Candidates) > 0 {
		// thinking モデルは parts[0] に thought パートが来る場合があるため、
		// thought フラグが false の最初のテキストパートを実際の返答として使用する
		for _, part := range geminiResp.Candidates[0].Content.Parts {
			if !part.Thought && part.Text != "" {
				replyText = part.Text
				break
			}
		}
	}

	utils.RespondWithJSON(w, AIChatResponse{Reply: replyText}, http.StatusOK)
	log.Printf("[AIChatHandler] AI response served successfully")
}
