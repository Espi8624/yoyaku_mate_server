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
# サーバー実行時にこのフォルダから設定ファイルを読み取れる
COPY --from=build /src/config /config

# Run in non-interactive mode for Infisical install
# jq 追加 (JSON parse)
RUN apk add --no-cache curl bash jq && \
    curl -1sLf 'https://dl.cloudsmith.io/public/infisical/infisical-cli/setup.alpine.sh' | bash && \
    apk add infisical

# 8080portを公開
EXPOSE 8080

# サーバー実行 (Infisicalを通じて実行, 環境変数で環境を指定)
# 1. APIを使用してトークン発行(CLIログイン問題回避)
# 2. 発行されたトークンでrun実行
CMD sh -c "export INFISICAL_TOKEN=\$(curl --silent --location --request POST 'https://app.infisical.com/api/v1/auth/universal-auth/login' \
    --header 'Content-Type: application/x-www-form-urlencoded' \
    --data-urlencode \"clientId=\${INFISICAL_CLIENT_ID}\" \
    --data-urlencode \"clientSecret=\${INFISICAL_CLIENT_SECRET}\" | jq -r .accessToken) && \
    infisical run --token=\${INFISICAL_TOKEN} --projectId=\${INFISICAL_PROJECT_ID} --env=\${INFISICAL_ENV:-dev} -- /saboten-server"