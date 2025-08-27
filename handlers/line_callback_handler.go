package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"github.com/line/line-bot-sdk-go/v8/linebot"
	"go.mongodb.org/mongo-driver/bson"
)

// LINEログイン後、リダイレクトされるリクエストを処理
// ユーザーがLINEで認証を完了したら、LINEプラットフォームがこのハンドラを呼出
func LineCallbackHandler(w http.ResponseWriter, r *http.Request) {
	// LINEが送信したデータ(code, state)を受け取る
	// ’state’はCSRF攻撃を防止するためのセキュリティトークン
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	// code又はstateが無い場合、不正なアクセスとしてブロックする
	if code == "" || state == "" {
		http.Error(w, "間違った要請です : 必須パラメータが存在しません", http.StatusBadRequest)
		return
	}

	// state tokenはCSRF攻撃を防止するために使用
	// SignUp時、DBに保存したトークンとLINEが返したトークンが一致するか確認
	licenseCollection := db.GetCollection(DatabaseName, "store_license")
	var storeLicense models.StoreLicense
	filter := bson.M{"line_auth_token": state}

	err := licenseCollection.FindOne(r.Context(), filter).Decode(&storeLicense)
	if err != nil {
		log.Printf("間違ったstateトークンです : %s, エラー : %v", state, err)
		http.Error(w, "セッションが切れたり、間違っています。再度会員加入をお願いします。", http.StatusUnauthorized)
		return
	}

	// 仮の'code'を実際の'id_token'に交換
	// サーバー間で安全に通信してトークンを取得
	// access_tokenは現在必要なため、ブランク識別子(_)で無視する
	_, idToken, err := exchangeCodeForTokens(code)
	if err != nil {
		http.Error(w, "LINEトークン交換に失敗しました。", http.StatusInternalServerError)
		return
	}

	// ’id_token'を検証し、本当のユーザーの固有ID(UserID)を検索
	lineUserID, err := verifyIDTokenAndGetUserID(idToken)
	if err != nil || lineUserID == "" {
		http.Error(w, "LINEユーザー情報確認に失敗しました。", http.StatusInternalServerError)
		return
	}

	// 検索したUserIDをDBに保存し、店舗情報と連結
	update := bson.M{
		"$set":   bson.M{"line_user_id": lineUserID},
		"$unset": bson.M{"line_auth_token": ""}, // セキュリティーのため、使用したトークンは削除
	}
	_, err = licenseCollection.UpdateOne(r.Context(), filter, update)
	if err != nil {
		http.Error(w, "ユーザー情報アップデートに失敗しました。", http.StatusInternalServerError)
		return
	}

	// 連結されたユーザーにLINEで最初の確認メッセージを送信
	sendLineConfirmationMessage(lineUserID, storeLicense.StoreID)

	// フロントエンドの完了ページにリダイレクト
	redirectURL := os.Getenv("FRONTEND_SIGNUP_COMPLETE_URL")
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// codeをaccess_tokenとid_tokenに交換する
func exchangeCodeForTokens(code string) (accessToken, idToken string, err error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {os.Getenv("LINE_CALLBACK_URL")},
		"client_id":     {os.Getenv("LINE_LOGIN_CHANNEL_ID")},
		"client_secret": {os.Getenv("LINE_LOGIN_CHANNEL_SECRET")},
	}

	resp, err := http.PostForm("https://api.line.me/oauth2/v2.1/token", data)
	if err != nil {
		log.Printf("トークン交換要請エラー : %v", err)
		return "", "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("LINEトークンAPIエラー : %s", string(body))
		return "", "", fmt.Errorf("LINE API非正常応答: %s", resp.Status)
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	json.Unmarshal(body, &tokenResponse)
	return tokenResponse.AccessToken, tokenResponse.IDToken, nil
}

// id_tokenを検証し、UserIDを抽出
func verifyIDTokenAndGetUserID(idToken string) (userID string, err error) {
	data := url.Values{
		"id_token":  {idToken},
		"client_id": {os.Getenv("LINE_LOGIN_CHANNEL_ID")},
	}

	resp, err := http.PostForm("https://api.line.me/oauth2/v2.1/verify", data)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("IDトークン検証要請エラー : %v", err)
		return "", fmt.Errorf("IDトークン検証失敗")
	}
	defer resp.Body.Close()

	var verifyResponse struct {
		UserID string `json:"sub"` // 'sub' フィールドにUserIDが含まれる
	}
	json.NewDecoder(resp.Body).Decode(&verifyResponse)
	return verifyResponse.UserID, nil
}

// ユーザーに'ボタンテンプレート'確認メッセージを送信
func sendLineConfirmationMessage(lineUserID, storeID string) {
	// DBで店舗情報を取得し、メッセージに店舗名を含める
	var store models.Store
	db.GetCollection(DatabaseName, StoresCollection).FindOne(context.TODO(), bson.M{"store_id": storeID}).Decode(&store)
	storeName := store.StoreName
	if storeName == "" {
		storeName = "申請店舗"
	}

	// messageText := fmt.Sprintf("はじめまして! '%s' ウェイティングサービスに申請しましたか？（もし、申請して無いであれば無視してください。）", storeName)
	titleText := fmt.Sprintf("「%s」店舗登録確認", storeName)
	bodyText := "申請内容でお間違いない場合、下のボタンを押して認証を完了してください。"

	if len([]rune(titleText)) > 40 {
		titleText = string([]rune(titleText)[:37]) + "..."
	}
	if len([]rune(bodyText)) > 60 {
		bodyText = string([]rune(bodyText)[:57]) + "..."
	}

	// 送るデータを定義
	postbackData := fmt.Sprintf("action=confirm_store&store_id=%s", storeID)

	// "はい"ボタン生成 (PostbackAction使用)
	confirmButton := linebot.NewPostbackAction("はい、間違いありません。", postbackData, "", "", "", "")

	// ボタンテンプレートメッセージを生成
	template := linebot.NewButtonsTemplate(
		"",            // image URL
		titleText,     // title
		bodyText,      // 本文テキスト
		confirmButton, // ボタン
	)

	// 実際送るメッセージオブジェクトを生成
	message := linebot.NewTemplateMessage("店舗登録確認メッセージが届きました。", template)

	// PushMessage APIを使用し、メッセージをユーザーに送信
	if _, err := bot.PushMessage(lineUserID, message).Do(); err != nil {
		log.Printf("LINEボタンテンプレートメッセージ発送失敗 (UserID: %s): %v", lineUserID, err)
	} else {
		log.Printf("確認メッセージを発送しました (受信者: %s)", lineUserID)
	}
}
