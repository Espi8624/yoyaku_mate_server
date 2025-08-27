package handlers

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"github.com/line/line-bot-sdk-go/v8/linebot"
	"github.com/line/line-bot-sdk-go/v8/linebot/webhook"
	"go.mongodb.org/mongo-driver/bson"
)

var bot *linebot.Client
var lineChannelSecret string

func InitLineBot(channelSecret, channelToken string) error {
	var err error
	bot, err = linebot.New(channelSecret, channelToken)
	if err != nil {
		log.Printf("LineBot初期化失敗 : %v", err)
		return err
	}
	lineChannelSecret = channelSecret
	log.Printf("LineBot初期化成功")
	return nil
}

func LineWebhookHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Webhook要請受信 : %s %s", r.Method, r.URL.String())

	cb, err := webhook.ParseRequest(lineChannelSecret, r)
	if err != nil {
		log.Printf("Webhook要請パーシング失敗 : %v", err)
		if err.Error() == "invalid signature" {
			http.Error(w, "Invalid signature", http.StatusBadRequest)
		} else {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	log.Printf("Webhook パーシング成功, イベント数: %d", len(cb.Events))

	for _, event := range cb.Events {
		switch e := event.(type) {

		// テキストメッセージ受信
		case webhook.MessageEvent:
			// テキストメッセージ以外は無視
			handleMessageEvent(&e)

		// ボタン押下
		case webhook.PostbackEvent:
			handlePostbackEvent(&e)
		}
	}

	w.WriteHeader(http.StatusOK)
}

func handleMessageEvent(e *webhook.MessageEvent) {
	// テキストメッセージ以外は無視
	if _, ok := e.Message.(webhook.TextMessageContent); ok {
		var userID string
		switch source := e.Source.(type) {
		case webhook.UserSource:
			userID = source.UserId
		default:
			log.Printf("処理出来ないソースタイプ : %T", source)
			return
		}

		// DBで、PENDING状態の店舗があるか確認
		licenseCollection := db.GetCollection(DatabaseName, "store_license")
		var storeLicense models.StoreLicense
		filter := bson.M{
			"line_user_id":        userID,
			"verification_status": models.StatusPending,
		}

		err := licenseCollection.FindOne(context.Background(), filter).Decode(&storeLicense)

		// PENDING状態の店舗を見つかった場合、テキスト入力に応答
		if err == nil {
			receivedText := e.Message.(webhook.TextMessageContent).Text
			log.Printf("PENDING状態の使用者がテキスト入力 : '%s' (UserID: %s)", receivedText, userID)

			if receivedText == "はい" {
				// 使用者が「はい」と入力した場合、ボタンメッセージを再送する
				// 以前CallbackHandlerのために作成した関数を再利用
				log.Printf("ボタンメッセージを再送します (UserID: %s, StoreID: %s)", userID, storeLicense.StoreID)
				// line_callback_handler.goに定義された関数
				sendLineConfirmationMessage(userID, storeLicense.StoreID)
				// 参考: sendLineConfirmationMessageはPush APIを使用する為、ReplyTokenが不要
			} else {
				// 『はい』以外のテキストを入力した場合、ボタンを押すように案内する
				replyText := "申し訳ございません。確認のため「はい」ボタンを押して下さい。"
				replyMessage := linebot.NewTextMessage(replyText)
				if _, err := bot.ReplyMessage(e.ReplyToken, replyMessage).Do(); err != nil {
					log.Printf("ボタンクリックメッセージ送信失敗 (UserID: %s): %v", userID, err)
				}
			}
		} else {
			// PENDING状態の店舗を見つからなかった場合
			log.Printf("PENDINGではない状態の使用者がテキストを入力 (UserID: %s)", userID)

			// どのテキストを入力しても一貫した案内メッセージを送信
			replyText := "現在申請した内容を管理者が検討しているか、すでに処理を完了しています。"
			replyMessage := linebot.NewTextMessage(replyText)
			if _, err := bot.ReplyMessage(e.ReplyToken, replyMessage).Do(); err != nil {
				log.Printf("検討中案内メッセージ送信失敗 (UserID: %s): %v", userID, err)
			}
		}
	}
}

// Postbackイベントを処理
func handlePostbackEvent(e *webhook.PostbackEvent) {
	var userID string

	// sourceからUserIDを抽出 (値タイプで処理)
	switch source := e.Source.(type) {
	case webhook.UserSource:
		userID = source.UserId
	default:
		log.Printf("処理出来ないsourceタイプ : %T", source)
		return
	}
	data := e.Postback.Data // ボタンに隠されていたデータ

	// Postbackデータをパースし、どのアクション・店舗か確認
	parsedData, err := url.ParseQuery(data)
	if err != nil {
		log.Printf("Postbackデータパース失敗 (UserID: %s, Data: %s): %v", userID, data, err)
		return
	}

	action := parsedData.Get("action")
	storeID := parsedData.Get("store_id")

	// actionが"confirm_store"で、store_idが存在する場合に処理
	if action == "confirm_store" && storeID != "" {
		log.Printf("店舗確認ボタンクリック受信 (UserID: %s, StoreID: %s)", userID, storeID)

		// storeIDとuserIDを使用して、該当する店舗の状態を更新
		licenseCollection := db.GetCollection(DatabaseName, "store_license")
		filter := bson.M{
			"line_user_id":        userID,
			"store_id":            storeID,
			"verification_status": models.StatusPending, // PENDING状態のデータのみ更新
		}
		update := bson.M{"$set": bson.M{"verification_status": models.StatusPendingReview}}

		result, err := licenseCollection.UpdateOne(context.Background(), filter, update)
		if err != nil {
			log.Printf("DBステータスアップデート失敗 (UserID: %s, StoreID: %s): %v", userID, storeID, err)
			return
		}

		if result.ModifiedCount > 0 {
			// 処理完了メッセージを使用者に返信
			replyText := "申請が正常に確認されました。管理者が検討を開始します。"
			replyMessage := linebot.NewTextMessage(replyText)
			if _, err := bot.ReplyMessage(e.ReplyToken, replyMessage).Do(); err != nil {
				log.Printf("Postbackメッセージ返信失敗 (UserID: %s): %v", userID, err)
			}
		} else {
			// すでに処理された要請である場合
			log.Printf("アップデートする店舗が見つかってない、又は、すでに処理されている (UserID: %s, StoreID: %s)", userID, storeID)
			replyText := "既に処理された店舗です。" // 重複クリック防止案内
			replyMessage := linebot.NewTextMessage(replyText)
			bot.ReplyMessage(e.ReplyToken, replyMessage).Do()
		}
	}
}
