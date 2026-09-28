#!/bin/bash
# iCloud 协议登录完整脚本（含 device_fingerprint）
# 用法：./icloud-login-with-fingerprint.sh YOUR_APPLE_ID YOUR_PASSWORD

set -euo pipefail

BASE="${ICLOUD_API_BASE:-http://127.0.0.1:8080}"
AUTH_HEADER="Authorization: Bearer ${ADMIN_KEY:-admin-secret-key}"

APPLE_ID="${1:-}"
PASSWORD="${2:-}"

if [[ -z "$APPLE_ID" || -z "$PASSWORD" ]]; then
    echo "用法：$0 <apple_id> <password>"
    echo "示例：$0 example@icloud.com your_password"
    exit 1
fi

# 生成随机设备指纹（16 字符十六进制）
DEVICE_FINGERPRINT=$(head -c 8 /dev/urandom | xxd -p)
DEVICE_NAME="gptgrok2api-${DEVICE_FINGERPRINT}"

echo "=== iCloud 协议登录（带设备指纹）==="
echo "Apple ID: $APPLE_ID"
echo "设备名称: $DEVICE_NAME"
echo "设备指纹: $DEVICE_FINGERPRINT"
echo "======================================"
echo ""

# Step 1: 发起登录
echo "[Step 1] 发起协议登录..."
LOGIN_RESPONSE=$(curl -s -X POST \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d "{
        \"apple_id\": \"$APPLE_ID\",
        \"password\": \"$PASSWORD\",
        \"two_factor_method\": \"app\",
        \"device_name\": \"$DEVICE_NAME\",
        \"device_fingerprint\": \"$DEVICE_FINGERPRINT\"
    }" \
    "$BASE/api/icloud/icloud/protocol-login/start")

echo "登录响应:"
echo "$LOGIN_RESPONSE" | jq '.' 2>/dev/null || echo "$LOGIN_RESPONSE"
echo ""

# 检查是否需要 2FA
NEEDS_2FA=$(echo "$LOGIN_RESPONSE" | jq -r '.needs_2fa // false')

if [[ "$NEEDS_2FA" == "true" ]]; then
    PENDING_ID=$(echo "$LOGIN_RESPONSE" | jq -r '.pending_id // ""')
    METHOD=$(echo "$LOGIN_RESPONSE" | jq -r '.method // .two_factor_challenge_type // "unknown"')
    EXPIRES_AT=$(echo "$LOGIN_RESPONSE" | jq -r '.expires_at // "unknown"')
    
    echo "⚠️  需要 2FA 验证"
    echo "   Pending ID: $PENDING_ID"
    echo "   方法：$METHOD"
    echo "   过期时间：$EXPIRES_AT"
    echo ""
    
    # 提示用户输入验证码
    read -p "请输入 6 位验证码： " CODE
    
    if [[ ${#CODE} -ne 6 || ! "$CODE" =~ ^[0-9]{6}$ ]]; then
        echo "错误：验证码必须是 6 位数字"
        exit 1
    fi
    
    echo ""
    echo "[Step 2] 提交 2FA 验证码..."
    
    # 根据方法选择提交方式
    case "$METHOD" in
        sms|phone|phone_sms|trusted_phone*)
            SUBMIT_METHOD="SMS"
            ;;
        trusted_device|app|*)
            SUBMIT_METHOD="trusted_device"
            ;;
    esac
    
    2FA_RESPONSE=$(curl -s -X POST \
        -H "$AUTH_HEADER" \
        -H "Content-Type: application/json" \
        -d "{
            \"pending_id\": \"$PENDING_ID\",
            \"code\": \"$CODE\",
            \"method\": \"$SUBMIT_METHOD\"
        }" \
        "$BASE/api/icloud/icloud/protocol-login/2fa")
    
    echo "2FA 响应:"
    echo "$2FA_RESPONSE" | jq '.' 2>/dev/null || echo "$2FA_RESPONSE"
    echo ""
    
    LOGIN_RESPONSE="$2FA_RESPONSE"
fi

# Step 3: 检查最终状态
SUCCESS=$(echo "$LOGIN_RESPONSE" | jq -r '.success // false')

if [[ "$SUCCESS" == "true" ]]; then
    echo "✅ 登录成功!"
    echo ""
    
    # 显示会话信息
    if echo "$LOGIN_RESPONSE" | jq -e '.session' >/dev/null 2>&1; then
        SESSION_INFO=$(echo "$LOGIN_RESPONSE" | jq '.session')
        echo "会话信息:"
        echo "$SESSION_INFO" | jq '.'
        echo ""
        
        ICLOUD_WEB_SAVED=$(echo "$SESSION_INFO" | jq -r '.icloud_web_login_saved // false')
        APPLE_ACCOUNT_SAVED=$(echo "$SESSION_INFO" | jq -r '.apple_account_login_saved // false')
        
        if [[ "$ICLOUD_WEB_SAVED" == "true" ]]; then
            echo "✅ iCloud Web 登录态已保存"
        fi
        
        if [[ "$APPLE_ACCOUNT_SAVED" == "true" ]]; then
            echo "✅ Apple Account 登录态已保存"
        fi
    elif echo "$LOGIN_RESPONSE" | jq -e '.saved' >/dev/null 2>&1; then
        SAVED=$(echo "$LOGIN_RESPONSE" | jq -r '.saved')
        if [[ "$SAVED" == "true" ]]; then
            echo "✅ 登录态已保存"
        fi
    fi
    
    # 验证登录态
    echo ""
    echo "[验证] 检查登录态..."
    STATUS_RESPONSE=$(curl -s -H "$AUTH_HEADER" "$BASE/api/icloud/status")
    echo "状态:"
    echo "$STATUS_RESPONSE" | jq '.icloud_session' 2>/dev/null || echo "$STATUS_RESPONSE"
    
else
    ERROR_CODE=$(echo "$LOGIN_RESPONSE" | jq -r '.code // "unknown"')
    ERROR_MSG=$(echo "$LOGIN_RESPONSE" | jq -r '.message // "未知错误"')
    
    echo "❌ 登录失败"
    echo "   错误代码：$ERROR_CODE"
    echo "   错误信息：$ERROR_MSG"
    echo ""
    
    case "$ERROR_CODE" in
        apple_credentials_invalid)
            echo "💡 建议：检查 Apple ID 和密码是否正确"
            ;;
        apple_login_forbidden)
            echo "💡 建议：账号可能被限制登录，请检查 Apple ID 安全状态"
            ;;
        invalid_2fa_code)
            echo "💡 建议：验证码格式不正确，请确保输入 6 位数字"
            ;;
        apple_2fa_failed)
            echo "💡 建议：验证码错误或已过期，请重新发起登录"
            ;;
        security_lockout)
            RETRY_AFTER=$(echo "$LOGIN_RESPONSE" | jq -r '.retry_after_seconds // 300')
            echo "💡 建议：账号已暂时锁定，请等待 ${RETRY_AFTER} 秒后重试"
            ;;
        *)
            echo "💡 建议：查看日志或联系技术支持"
            ;;
    esac
    
    exit 1
fi

echo ""
echo "======================================"
echo "登录流程完成!"
echo "======================================"
