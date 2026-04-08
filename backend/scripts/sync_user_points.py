#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
sync_user_points.py

数据清洗脚本：将所有用户的 User 表冗余积分字段与 Redemption 流水表强制对齐。

使用场景：
  1. 首次部署积分同步重构后，修复历史数据中可能存在的不一致问题。
  2. 任何怀疑积分数据不一致时，可作为修复工具运行。

使用方法：
  # 安装依赖（仅需一次）
  pip install PyMySQL python-dotenv

  # 运行脚本（在 backend 目录下执行）
  python3 scripts/sync_user_points.py

  # 也可通过环境变量指定数据库连接（优先级高于默认值）
  MYSQL_HOST=127.0.0.1 MYSQL_PORT=3306 MYSQL_USER=root \\
  MYSQL_PASSWORD=quizapp2026 MYSQL_DB=quiz_app \\
  python3 scripts/sync_user_points.py

  # 或使用完整 DSN
  MYSQL_DSN="root:quizapp2026@127.0.0.1:3306/quiz_app" \\
  python3 scripts/sync_user_points.py

注意：
  - 脚本会打印每个用户的修复情况，请在执行前备份数据库。
  - 脚本是幂等的，可以安全地多次执行。
  - 与 Go 版本逻辑完全一致，数据来源均为 Redemption 流水表。
"""

import os
import sys
import logging
import re

try:
    import pymysql
except ImportError:
    print("[ERROR] 缺少依赖：请先执行 pip install PyMySQL")
    sys.exit(1)

# ── 日志配置 ──────────────────────────────────────────────────────────────────
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s  %(levelname)s  %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
log = logging.getLogger(__name__)


# ── 数据库连接 ─────────────────────────────────────────────────────────────────
def get_connection() -> pymysql.Connection:
    """
    按照与 Go 版本相同的优先级读取数据库配置：
      1. MYSQL_DSN 环境变量（完整 DSN 字符串）
      2. 各独立环境变量（MYSQL_HOST / PORT / USER / PASSWORD / DB）
      3. 代码内默认值（与 config/database.go 保持一致）
    """
    dsn = os.environ.get("MYSQL_DSN", "")
    if dsn:
        # 解析 DSN 格式：user:password@host:port/dbname
        m = re.match(r"^(.+?):(.*)@(.+?):(\d+)/(.+)$", dsn)
        if not m:
            log.error("MYSQL_DSN 格式无效，期望格式：user:password@host:port/dbname")
            sys.exit(1)
        user, password, host, port, dbname = m.groups()
        port = int(port)
    else:
        host     = os.environ.get("MYSQL_HOST",     "127.0.0.1")
        port     = int(os.environ.get("MYSQL_PORT", "3306"))
        user     = os.environ.get("MYSQL_USER",     "root")
        password = os.environ.get("MYSQL_PASSWORD", "quizapp2026")
        dbname   = os.environ.get("MYSQL_DB",       "quiz_app")

    log.info("连接数据库 %s:%d/%s (user=%s) ...", host, port, dbname, user)
    try:
        conn = pymysql.connect(
            host=host,
            port=port,
            user=user,
            password=password,
            database=dbname,
            charset="utf8mb4",
            autocommit=False,
            cursorclass=pymysql.cursors.DictCursor,
        )
        log.info("数据库连接成功")
        return conn
    except pymysql.Error as e:
        log.error("数据库连接失败: %s", e)
        sys.exit(1)


# ── 核心逻辑 ───────────────────────────────────────────────────────────────────
def sync_user_points(conn: pymysql.Connection) -> None:
    """
    遍历所有非管理员用户，从 Redemption 流水表重新计算各类积分，
    若与 User 表冗余字段不一致则执行修复。

    与 Go 版本 sync_user_points.go 逻辑完全一致：
      quiz_score      = SUM(points) WHERE type='quiz'     AND status='success'
      activity_points = SUM(points) WHERE type='activity' AND status='success'
      used_points     = SUM(points) WHERE type='redeem'   AND status='success'
      points          = quiz_score + activity_points - used_points  (最小值为 0)
    """
    log.info("=== 开始同步用户积分冗余字段 ===")

    cursor = conn.cursor()

    # 获取所有非管理员用户
    # users 表有 deleted_at 字段（GORM 软删除），需要过滤已删除用户
    cursor.execute(
        "SELECT id, employee_id, name, quiz_score, activity_points, used_points, points "
        "FROM users WHERE is_admin = 0 AND deleted_at IS NULL"
    )
    users = cursor.fetchall()
    log.info("共找到 %d 个用户，开始逐一同步...", len(users))

    fixed_count = 0
    error_count = 0

    for user in users:
        uid         = user["id"]
        employee_id = user["employee_id"]
        name        = user["name"]

        try:
            # ── 从 Redemption 表计算各类积分（与 SyncUserPointsTx 逻辑一致）──

            # redemptions 表没有 deleted_at 字段（流水表不做软删除）
            cursor.execute(
                "SELECT COALESCE(SUM(points), 0) AS total "
                "FROM redemptions "
                "WHERE user_id = %s AND type = 'quiz' AND status = 'success'",
                (uid,)
            )
            quiz_score = cursor.fetchone()["total"]

            cursor.execute(
                "SELECT COALESCE(SUM(points), 0) AS total "
                "FROM redemptions "
                "WHERE user_id = %s AND type = 'activity' AND status = 'success'",
                (uid,)
            )
            activity_points = cursor.fetchone()["total"]

            cursor.execute(
                "SELECT COALESCE(SUM(points), 0) AS total "
                "FROM redemptions "
                "WHERE user_id = %s AND type = 'redeem' AND status = 'success'",
                (uid,)
            )
            used_points = cursor.fetchone()["total"]

            available_points = quiz_score + activity_points - used_points
            if available_points < 0:
                available_points = 0

            # ── 检查是否需要修复 ──────────────────────────────────────────────
            needs_fix = (
                user["quiz_score"]      != quiz_score      or
                user["activity_points"] != activity_points or
                user["used_points"]     != used_points     or
                user["points"]          != available_points
            )

            if not needs_fix:
                continue

            # ── 执行修复（单条 UPDATE，在事务中完成）────────────────────────
            cursor.execute(
                "UPDATE users SET "
                "  quiz_score = %s, "
                "  activity_points = %s, "
                "  used_points = %s, "
                "  points = %s "
                "WHERE id = %s",
                (quiz_score, activity_points, used_points, available_points, uid)
            )
            conn.commit()

            print(f"[FIXED] 用户 {name:<10} ({employee_id})")
            print(f"        quiz_score:      {user['quiz_score']} -> {quiz_score}")
            print(f"        activity_points: {user['activity_points']} -> {activity_points}")
            print(f"        used_points:     {user['used_points']} -> {used_points}")
            print(f"        points:          {user['points']} -> {available_points}")
            fixed_count += 1

        except pymysql.Error as e:
            conn.rollback()
            log.error("[ERROR] 用户 %s (%s) 修复失败: %s", name, employee_id, e)
            error_count += 1
            continue

    cursor.close()

    print()
    log.info("=== 同步完成 ===")
    log.info("总用户数: %d", len(users))
    log.info("已修复:   %d", fixed_count)
    log.info("无需修复: %d", len(users) - fixed_count - error_count)
    log.info("修复失败: %d", error_count)

    if error_count > 0:
        sys.exit(1)


# ── 入口 ───────────────────────────────────────────────────────────────────────
if __name__ == "__main__":
    conn = get_connection()
    try:
        sync_user_points(conn)
    finally:
        conn.close()
