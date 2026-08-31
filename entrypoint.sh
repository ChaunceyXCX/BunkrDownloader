#!/bin/sh
# ============================================================================
#  BunkrDownloader · 启动入口
#  确保持久化目录存在且可写后再启动应用。
#  注意：对于宿主机 bind mount，权限由宿主机控制，容器内无法 chown。
# ============================================================================
set -e

# 确保 /data 和 /downloads 存在且可写
for dir in /data /downloads; do
    if [ ! -d "$dir" ]; then
        mkdir -p "$dir"
    fi
    # bind mount 场景：权限由宿主机控制，跳过 chown
    # 仅对全新创建的目录（非 mount）才尝试修复所有权
    if ! mountpoint -q "$dir" 2>/dev/null; then
        if [ "$(id -u)" -ne 0 ] && [ "$(stat -c '%u' "$dir" 2>/dev/null)" != "$(id -u)" ]; then
            chown -R "$(id -u):$(id -g)" "$dir"
        fi
    fi
done

exec "$@"
