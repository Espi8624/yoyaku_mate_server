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

# 8080portを公開
EXPOSE 8080

# サーバー実行。
#
# シークレットは Infisical の Fly.io Secret Sync が fly secrets へ push したものを、
# 通常の環境変数として受け取る。このコンテナは Infisical に一切アクセスしない。
#
# 以前はここで毎起動 app.infisical.com へログインしてトークンを取り、
# `infisical run -- /saboten-server` として起動していた。やめた理由:
#
#   - min_machines_running = 0 のためマシンは頻繁に起動し直す。そのたびに外部APIへ
#     依存しており、Infisical が落ちていればサーバーが起動できなかった
#   - `curl ... | jq` はパイプの終了コードが jq のものになるため、curl が失敗しても
#     トークンが "null" のまま処理が進み、失敗が握り潰されていた
#     (同じ罠でこのファイルは2026-09-17に一度ビルドが停止している)
#   - CLI の取得でビルドが外部配布物に依存していた
#
# Infisical は引き続きシークレットの単一ソースであり、チームのローカル開発も
# `infisical run` のまま変わらない。変わったのは注入経路だけ。
#
# 注意: ca-certificates は上で別途インストールしている。MongoDB Atlas・Gemini・R2 の
# TLS 接続に必須なので、この行を整理する際に巻き込んで消さないこと。
CMD ["/saboten-server"]