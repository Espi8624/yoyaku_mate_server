// シフト表の下書き/確定版分離に伴う一度きりの移行プログラム。
//
//	go run ./scripts/migrate_shift_table_publish            # 対象件数の確認のみ (既定)
//	go run ./scripts/migrate_shift_table_publish -apply     # 実際に更新する
//
// 背景:
//   - ShiftTable に published_shifts / published_at を追加し、スタッフには確定版だけを
//     見せるようにした。既存ドキュメントには published_at が無いため、この移行を実行せずに
//     新サーバーをデプロイすると、公開済みだったシフト表が全スタッフから見えなくなる
//     (published_at == nil = 一度も確定していない、と判定されるため)。
//   - よってサーバーのデプロイより先に実行すること。現時点で存在するシフト表は全て
//     公開済みだった扱いにし、下書きをそのまま確定版へ写す。
//
// 冪等性: published_at を持たないドキュメントだけを対象にするため、複数回実行しても安全。
// 接続先: config/{GO_ENV}.json の mongodb.uri (既定は development)。
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"yoyaku_mate_server/config"
	"yoyaku_mate_server/data"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	apply := flag.Bool("apply", false, "実際に更新する (未指定なら対象件数の確認のみ)")
	flag.Parse()

	cfg := config.Load()
	if cfg.MongoDB.URI == "" {
		log.Fatal("mongodb.uri が設定されていません")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoDB.URI))
	if err != nil {
		log.Fatalf("MongoDBへの接続に失敗しました: %v", err)
	}
	defer func() {
		if err := client.Disconnect(context.Background()); err != nil {
			log.Printf("切断に失敗しました: %v", err)
		}
	}()

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("MongoDBへのPingに失敗しました: %v", err)
	}

	collection := client.Database(cfg.MongoDB.Database).Collection(data.CollectionShiftTables)
	log.Printf("接続先: database=%s collection=%s", cfg.MongoDB.Database, data.CollectionShiftTables)

	// published_at を持たないドキュメントだけが移行対象
	target := bson.M{"published_at": bson.M{"$exists": false}}

	total, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Fatalf("総件数の取得に失敗しました: %v", err)
	}
	before, err := collection.CountDocuments(ctx, target)
	if err != nil {
		log.Fatalf("対象件数の取得に失敗しました: %v", err)
	}
	log.Printf("シフト表: 全%d件 / 移行対象 %d件", total, before)

	if before == 0 {
		log.Println("移行対象がありません (実行済み、またはデータなし)")
		return
	}
	if !*apply {
		log.Println("確認のみで終了します。実際に更新するには -apply を付けて再実行してください")
		return
	}

	// $set にパイプラインを使い、既存フィールドの値をそのまま写す
	update := mongo.Pipeline{
		{{Key: "$set", Value: bson.M{
			// shifts が未設定のドキュメントもあるため、必ず配列に落としてから写す
			"published_shifts": bson.M{"$ifNull": bson.A{"$shifts", bson.A{}}},
			// 公開日時は既存の更新日時を流用する (無ければ作成日時)
			"published_at": bson.M{"$ifNull": bson.A{"$updated_at", "$created_at"}},
		}}},
	}

	result, err := collection.UpdateMany(ctx, target, update)
	if err != nil {
		log.Fatalf("移行に失敗しました: %v", err)
	}
	log.Printf("更新: %d件 (matched=%d)", result.ModifiedCount, result.MatchedCount)

	remaining, err := collection.CountDocuments(ctx, target)
	if err != nil {
		log.Fatalf("残件数の確認に失敗しました: %v", err)
	}
	if remaining == 0 {
		log.Println("完了: 全てのシフト表が確定版を持っています")
	} else {
		log.Printf("警告: %d件が未移行のまま残っています", remaining)
	}
}
