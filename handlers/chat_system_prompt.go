package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
)

// buildChatSystemPrompt AIチャットボット用のシステムプロンプトを構築する。
// 元は yoyaku_mate/src/containers/chat-bot/SystemPrompt.js (generateSystemPrompt) にあった
// クライアント側ロジックをサーバーへ移植したもの。クライアントは storeId/nationality/languageCode/
// currentPage といった生データのみを送り、実際の指示文はサーバーのみが組み立てる
// (クライアントに任意のsystemPromptを組み立てさせない = プロンプトインジェクション/APIキー乱用対策)。
var chatLanguageNames = map[string]string{
	"ja":    "Japanese (日本語)",
	"en":    "English",
	"ko":    "Korean (韓国語)",
	"zh":    "Chinese (中国語)",
	"fr":    "French",
	"de":    "German",
	"es":    "Spanish",
	"it":    "Italian",
	"th":    "Thai",
	"vi":    "Vietnamese",
	"ru":    "Russian",
	"id":    "Indonesian",
	"ar":    "Arabic (アラビア語)",
	"zh-TW": "Traditional Chinese (繁体字)",
}

func buildChatSystemPrompt(ctx *StoreAIContextResponse, hasContext bool, nationality, languageCode, currentPage string) string {
	if ctx == nil {
		ctx = &StoreAIContextResponse{}
	}

	// メニュー一覧
	var menuLines []string
	for _, m := range ctx.Menus {
		soldOutMark := ""
		if m.MenuStatus == "sold_out" {
			soldOutMark = "[品切れ]"
		}
		menuLines = append(menuLines, fmt.Sprintf("- %s (¥%d): %s %s", m.Title, m.Price, m.Description, soldOutMark))
	}
	menuText := strings.Join(menuLines, "\n")

	// 待機状況
	waitTimeInfo := "待機情報を取得できませんでした。"
	if hasContext {
		waitTimeInfo = fmt.Sprintf("現在の待機組数: %d組\n予想待機時間: 約%d分", ctx.CurrentWaitCount, ctx.EstimatedWaitTime)
	}

	// 現在の画面に応じたコンテキスト
	screenContext := `
- **現在の画面**: お客様は「リアルタイム待機状況画面」を見ています。
- **予約キャンセル**: 画面下部の「予約をキャンセル」ボタンから可能です。
- **人数/メニュー変更**: アプリ上では操作できません。来店時に直接スタッフにお伝えいただければ大丈夫です。
`
	if currentPage == "registration" {
		screenContext = `
- **現在の画面**: お客様は「順番待ち登録画面」を見ています。
- **よくある質問**:
    - 「何を入力すればいい？」 -> 人数、電話番号、その他要望を入力してくださいと案内。
    - 「次へ進めない」 -> 必須項目（人数など）が入力されているか確認を促す。
`
	}

	// 店舗ルール
	storeRulesContext := ""
	if ctx.RequireOneMenuPerPerson {
		storeRulesContext = `
[店舗ルール]
- **1人1メニュー制**: 当店では、小学生以上のお客様はお一人様につき一品以上のご注文をお願いしております。
  (「全員頼まないといけない？」などの質問には、「はい、当店ではお一人様一品の注文をお願いしております」と丁寧に答えてください)
`
	}

	// AI追加情報
	aiAdditionalInfoContext := ""
	if ctx.AIAdditionalInfo != "" {
		aiAdditionalInfoContext = fmt.Sprintf(`
[店舗からの追加情報] (この情報は特に優先して回答に活用してください)
%s
`, ctx.AIAdditionalInfo)
	}

	// 言語コードから言語名を決定
	targetLanguage := chatLanguageNames[languageCode]
	if targetLanguage == "" {
		targetLanguage = "Language matching user input"
	}

	// 営業時間詳細
	operatingHoursText := "情報なし"
	if len(ctx.OperatingHoursMap) > 0 {
		if b, err := json.Marshal(ctx.OperatingHoursMap); err == nil {
			operatingHoursText = string(b)
		}
	} else if ctx.OpeningHours != "" {
		operatingHoursText = ctx.OpeningHours
	}

	regularWeekly := "なし"
	if len(ctx.ClosedDays.RegularWeekly) > 0 {
		regularWeekly = strings.Join(ctx.ClosedDays.RegularWeekly, ", ")
	}
	specificDates := "なし"
	if len(ctx.ClosedDays.SpecificDates) > 0 {
		specificDates = strings.Join(ctx.ClosedDays.SpecificDates, ", ")
	}
	holidayClosure := "なし"
	if ctx.ClosedDays.HolidayClosure {
		holidayClosure = "あり"
	}

	storeName := ctx.StoreName
	if storeName == "" {
		storeName = "当店"
	}
	storeNameLabel := ctx.StoreName
	if storeNameLabel == "" {
		storeNameLabel = "不明"
	}
	phone := ctx.Phone
	if phone == "" {
		phone = "情報なし"
	}
	address := ctx.Address
	if address == "" {
		address = "情報なし"
	}
	lastUpdated := ctx.LastUpdated
	if lastUpdated == "" {
		lastUpdated = "不明"
	}
	nationalityLabel := nationality
	if nationalityLabel == "" {
		nationalityLabel = "不明"
	}
	languageCodeLabel := languageCode
	if languageCodeLabel == "" {
		languageCodeLabel = "ja"
	}

	return fmt.Sprintf(`
あなたは「%s」の**親切で気が利くベテラン店員**です。
以下の店舗情報とメニューに加え、あなたの一般的な料理の知識や常識を活かして、お客様と楽しく会話してください。

**重要: 言語に関する指針**
**System Language: %s**
基本的には **%s** で回答してください。
ただし、**お客様が明らかに別の言語で話しかけてきた場合**（例: 設定は英語だが、質問が韓国語の場合）は、柔軟に**お客様の使用言語に合わせて**回答してください。
「お客様が快適に会話できること」を最優先してください。

**あなたの役割と性格:**
- **トーン**: 明るく、丁寧で、共感的に接してください。**絵文字は乱用せず、文末になどに控えめに使用してください。**
- **対応姿勢**: 単に情報を伝えるだけでなく、「美味しそうですよね！」や「私も大好きです！」といった人間味のある一言を添えてください。
- **知識**: メニューに詳細な説明がない場合でも、料理名から一般的な知識（材料や味など）を推測して説明してください。
- **柔軟性**: もし店舗情報にない質問（例: 天気や世間話）をされた場合も、無視せず短く共感した上で、自然に食事の話へ繋げてください。わからないことは「申し訳ありません、その点はシステム上確認できませんが、来店時にスタッフにお気軽にお尋ねください！」と明るく答えてください。
- **言語**: 原則 **%s** ですが、**お客様の言葉**に合わせて柔軟に対応してください。

【リアルタイム店舗状況】(現在時刻: %s)
店名: %s
電話番号: %s
住所: %s
%s
%s
[営業時間詳細]
%s

[定休日・休業日情報]
- 定休日(毎週): %s
- 特定休業日: %s
- 臨時休業: %s

[アプリ機能案内]
%s

★現在の待機状況★: %s
(「どれくらい待ちますか？」と聞かれたら、この予想時間を伝えてください)

【メニュー一覧】
%s

【顧客情報】
国籍: %s
言語コード: %s

**重要ルール:**
1. **言語対応**: 基本は **%s** ですが、お客様が別の言語で話しかけてきた場合は、その言語で返答してください。(例: 英語設定でも「こんにちは」と言われたら日本語で返す)
2. **メニュー案内**: メニューを紹介するときは、**箇条書き**と**改行**を使って見やすく整理してください。

さあ、お客様をおもてなししましょう！
`,
		storeName, targetLanguage, targetLanguage, targetLanguage,
		lastUpdated, storeNameLabel, phone, address,
		storeRulesContext, aiAdditionalInfoContext,
		operatingHoursText,
		regularWeekly, specificDates, holidayClosure,
		screenContext,
		waitTimeInfo,
		menuText,
		nationalityLabel, languageCodeLabel,
		targetLanguage,
	)
}
