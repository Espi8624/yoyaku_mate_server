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

# Infisical CLI (起動時のシークレット注入に使用) と、トークン発行に必要な curl/jq。
#
# 以前は Cloudsmith の setup.alpine.sh を `curl ... | bash` して apk 経由で入れていたが、
# 2026-09-17 にそのスクリプトのURLが予告なく404になりビルドが停止した。
# さらに `A | B` の終了コードは B のものになるため、curl が 404 で失敗しても
# 空入力を受けた bash が 0 で終了し、失敗が握り潰されていた。その結果
# 「infisical (no such package)」という無関係なエラーに化けて原因が見えなくなっていた。
#
# 公式リリースのバイナリを**バージョン固定**で取得する。毎ビルド外部の最新物を
# 取りに行く構成をやめることで、配布側の都合でデプロイが止まるのを防ぐ。
ARG INFISICAL_VERSION=0.43.132
RUN apk add --no-cache curl bash jq && \
    case "$(uname -m)" in \
      x86_64) INFISICAL_ARCH=amd64 ;; \
      aarch64) INFISICAL_ARCH=arm64 ;; \
      *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;; \
    esac && \
    curl -fsSL -o /tmp/infisical.tar.gz \
      "https://github.com/Infisical/cli/releases/download/v${INFISICAL_VERSION}/cli_${INFISICAL_VERSION}_linux_${INFISICAL_ARCH}.tar.gz" && \
    tar -xzf /tmp/infisical.tar.gz -C /tmp infisical && \
    install -m 0755 /tmp/infisical /usr/local/bin/infisical && \
    rm -f /tmp/infisical.tar.gz /tmp/infisical && \
    infisical --version

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