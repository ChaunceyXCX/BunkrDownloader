#!/bin/sh
# ============================================================================
#  BunkrDownloader · 启动入口
#  确保持久化目录权限正确后再启动应用（解决 volume 挂载后 root 拥有权限问题）
# ============================================================================
set -e

# 确保 /data 和 /downloads 存在且属于当前用户
for dir in /data /downloads; do
    if [ ! -d "$dir" ]; then
        mkdir -p "$dir"
    fi
    # 如果目录不属于当前用户，修复所有权
    if [ "$(id -u)" -ne 0 ] && [ "$(stat -c '%u' "$dir" 2>/dev/null)" != "$(id -u)" ]; then
        chown -R "$(id -u):$(id -g)" "$dir"
    fi
done

exec "$@"
