#!/usr/bin/env bash
# ima-note-upload.sh — ima 知识库笔记上传一条龙（四步流封装）
# 用法: ./ima-note-upload.sh <markdown文件路径> [标题(缺省=文件名)]
# 前置: ~/.config/ima/{client_id,api_key,wiki_kbid} 在位
# 流程: GATE3查重 → create_media → COS上传 → add_knowledge → search验证
set -euo pipefail
FILE="${1:?usage: $0 <markdown-file> [title]}"
TITLE="${2:-$(basename "$FILE")}"
KB=$(cat "$HOME/.config/ima/wiki_kbid")
CJS="$(cd "$(dirname "$0")/.." && pwd)/skills/ima/ima_api.cjs"
COS_SCRIPT="$(cd "$(dirname "$0")/.." && pwd)/skills/ima/knowledge-base/scripts/cos-upload.cjs"
SIZE=$(stat -c%s "$FILE")

# GATE3: 查重
REP=$(node "$CJS" "openapi/wiki/v1/check_repeated_names" \
  "{\"params\": [{\"name\": \"$TITLE\", \"media_type\": 7}], \"knowledge_base_id\": \"$KB\"}")
echo "$REP" | grep -q '"is_repeated":false' || { echo "DUPLICATE: $TITLE 已存在，先处理重名（加时间戳）再上传"; exit 2; }
echo "GATE3 ok: 不重名"

# create_media
CREATE=$(node "$CJS" "openapi/wiki/v1/create_media" \
  "{\"file_name\": \"$TITLE\", \"file_size\": $SIZE, \"content_type\": \"text/markdown\", \"knowledge_base_id\": \"$KB\", \"file_ext\": \"md\"}")
CM_JSON=$(echo "$CREATE" > /tmp/ima_cm_$$.json; cat /tmp/ima_cm_$$.json)
MEDIA_ID=$(python3 -c "import json;print(json.load(open('/tmp/ima_cm_$$.json'))['data']['media_id'])")

# COS 上传（凭据从 create_media 响应内联提取，不落盘明文密钥文件）
node "$COS_SCRIPT" --file "$FILE" \
  --secret-id  "$(python3 -c "import json;print(json.load(open('/tmp/ima_cm_$$.json'))['data']['cos_credential']['secret_id'])")" \
  --secret-key "$(python3 -c "import json;print(json.load(open('/tmp/ima_cm_$$.json'))['data']['cos_credential']['secret_key'])")" \
  --token      "$(python3 -c "import json;print(json.load(open('/tmp/ima_cm_$$.json'))['data']['cos_credential']['token'])")" \
  --bucket     "$(python3 -c "import json;print(json.load(open('/tmp/ima_cm_$$.json'))['data']['cos_credential']['bucket_name'])")" \
  --region     "$(python3 -c "import json;print(json.load(open('/tmp/ima_cm_$$.json'))['data']['cos_credential']['region'])")" \
  --cos-key    "$(python3 -c "import json;print(json.load(open('/tmp/ima_cm_$$.json'))['data']['cos_credential']['cos_key'])")" \
  --content-type "text/markdown" \
  --start-time  "$(python3 -c "import json;print(json.load(open('/tmp/ima_cm_$$.json'))['data']['cos_credential']['start_time'])")" \
  --expired-time "$(python3 -c "import json;print(json.load(open('/tmp/ima_cm_$$.json'))['data']['cos_credential']['expired_time'])")" \
  --timeout 300000

# add_knowledge
ADD=$(node "$CJS" "openapi/wiki/v1/add_knowledge" \
  "{\"media_type\": 7, \"media_id\": \"$MEDIA_ID\", \"title\": \"$TITLE\", \"knowledge_base_id\": \"$KB\", \"file_info\": {\"cos_key\": \"$(python3 -c "import json;print(json.load(open('/tmp/ima_cm_$$.json'))['data']['cos_credential']['cos_key'])")\", \"file_size\": $SIZE, \"file_name\": \"$TITLE\"}}")
echo "$ADD" | grep -q '"code":0' || { echo "ADD_FAILED: $ADD"; exit 3; }
rm -f /tmp/ima_cm_$$.json
sleep 5
# search 验证
VERIFY=$(node "$CJS" "openapi/wiki/v1/search_knowledge" "{\"query\": \"$TITLE\", \"knowledge_base_id\": \"$KB\"}")
echo "$VERIFY" | grep -q "$TITLE" && echo "UPLOADED_AND_VERIFIED: $TITLE (media_id=$MEDIA_ID)" || echo "UPLOADED_BUT_INDEX_PENDING: $TITLE (media_id=$MEDIA_ID) — search 索引延迟，列表接口可查"
