# go言語コンパイラがインストールされた軽量Linux（alpine）イメージを使用
# この環境の別名を「build」に設定
FROM golang:1.23-alpine AS build

# 作業フォルダ
WORKDIR /src

# 最初に go.mod と go.sum ファイルだけコピー
COPY go.mod go.sum ./

# copyしたファイルを基に、プロジェクトに必要なすべてのライブラリを事前にダウンロード
RUN go mod download

# 残りのすべてのソースコード（.goファイル、configフォルダなど）をコピー
COPY . .

# go言語ソースコードをコンパイルし、'yoyaku-mate-server'という名前の実行ファイルを1つ作成
# CGO_ENABLED=0 オプションで、他のシステムライブラリなしで独立して実行できるファイルを作成
RUN CGO_ENABLED=0 go build -o /saboten-server .

# go こんパイラなどの開発ツールがすべて取り除かれた、軽量なLinux（alpine）イメージを使用
FROM alpine:latest

# 基本認証書をインストール
RUN apk add --no-cache ca-certificates

# 'build'環境でコンパイルした実行ファイルをコピー
COPY --from=build /saboten-server /saboten-server

# 'build'環境にコピーした'config'フォルダ全体をそのままコピー
# (config/*.jsonはgitignore対象のため実際にはexampleファイルのみ含まれる。
#  実行時の設定値はFly Secretsとして注入される環境変数で上書きされる)
COPY --from=build /src/config /config

# 8080portを公開
EXPOSE 8080

# サーバー実行 (設定はFly Secretsで注入された環境変数から読み込む)
CMD ["/saboten-server"]