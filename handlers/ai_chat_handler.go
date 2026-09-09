package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"yoyaku_mate_server/utils"
)

// AIChatRequest フロントエンドから受け取るリクエスト構造体。
// systemPromptは受け取らない — プロンプト自体はサーバー側(buildChatSystemPrompt)のみが組み立てる。
// クライアントに任意のsystemPromptを組み立てさせると、APIキーの用途をクライアント側で
// 好きに書き換えられてしまう(プロンプトインジェクション/キー乱用の穴になる)ため。
type AIChatRequest struct {
	StoreID      string `json:"storeId"`
	UserMessage  string `json:"userMessage"`
	Nationality  string `json:"nationality"`
	LanguageCode string `json:"languageCode"`
	CurrentPage  string `json:"currentPage"`
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

// AIChatHandler Gemini APIのプロキシハンドラ。
// システムプロンプトはstoreAIContextHandler経由でサーバーが自前で構築する
// (店舗情報/メニュー/待機状況もクライアントから受け取らず、サーバーがDBから直接取得する)。
type AIChatHandler struct {
	storeAIContextHandler *StoreAIContextHandler
}

// NewAIChatHandler AIChatHandlerのコンストラクタ
func NewAIChatHandler(storeAIContextHandler *StoreAIContextHandler) *AIChatHandler {
	return &AIChatHandler{storeAIContextHandler: storeAIContextHandler}
}

// Handle POST /api/public/ai-chat
func (h *AIChatHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if req.StoreID == "" {
		utils.RespondWithError(w, "storeId is required", http.StatusBadRequest)
		return
	}
	if req.UserMessage == "" {
		utils.RespondWithError(w, "userMessage is required", http.StatusBadRequest)
		return
	}

	// 店舗のリアルタイムコンテキスト (メニュー/待機状況/店舗設定) をサーバー内部で取得。
	// 取得に失敗しても (例: store_idが不正) チャット自体は継続し、コンテキストなしで応答する
	ctx, err := h.storeAIContextHandler.BuildContext(req.StoreID)
	hasContext := err == nil
	if err != nil {
		log.Printf("[AIChatHandler] Failed to build store context for %s: %v", req.StoreID, err)
	}

	systemPrompt := buildChatSystemPrompt(ctx, hasContext, req.Nationality, req.LanguageCode, req.CurrentPage)
	fullPrompt := systemPrompt + "\n\nお客様: " + req.UserMessage

	replyText, err := callGeminiForText(fullPrompt)
	if err != nil {
		if errors.Is(err, errGeminiRateLimited) {
			utils.RespondWithError(w, "AI service is busy. Please try again later.", http.StatusTooManyRequests)
			return
		}
		log.Printf("[AIChatHandler] %v", err)
		utils.RespondWithError(w, "AI service error", http.StatusBadGateway)
		return
	}
	if replyText == "" {
		replyText = "すみません、うまく聞き取れませんでした。"
	}

	utils.RespondWithJSON(w, AIChatResponse{Reply: replyText}, http.StatusOK)
	log.Printf("[AIChatHandler] AI response served successfully")
}
