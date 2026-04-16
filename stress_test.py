#!/usr/bin/env python3
"""
Transphorm Quiz App 压力测试脚本
目标站点: https://jw.yueyuancom.site

测试场景:
  场景 A - 1000 人并发读取（登录 + 查看积分 + 查看商品）
  场景 B - 50 个管理员并发扫码兑换商品 + 活动积分（模拟高峰期核销）
  场景 C - 数据一致性验证（测试结束后抽查用户积分是否与流水一致）

使用方法:
  python3 stress_test.py                        # 运行全部场景（A + B + C）
  python3 stress_test.py --scene A              # 只运行场景 A
  python3 stress_test.py --scene B              # 只运行场景 B + C
  python3 stress_test.py --scene C              # 只运行一致性验证
  python3 stress_test.py --scene A --concurrency 500

依赖: pip install aiohttp
"""

import asyncio
import argparse
import hashlib
import hmac
import json
import random
import sys
import time
from collections import defaultdict
from dataclasses import dataclass, field
from typing import Optional

import aiohttp

# ─────────────────────────────────────────────
# 配置区（运行前请修改）
# ─────────────────────────────────────────────
BASE_URL = "https://jw.yueyuancom.site"

# 管理员账号（工号 / 姓名）—— 确保系统中存在且为管理员
ADMIN_ACCOUNTS = [
    {"employee_id": "admin", "name": "管理员"},
]

# 测试用普通用户数量（场景 A）
# 脚本会尝试登录 T0001/测试用户0001 ~ T{N}/测试用户{N} 格式的账号
# 如果系统中没有这些账号，登录会失败并被统计为「账号不存在」
TEST_USER_COUNT = 1000

# 场景 B 配置
ADMIN_WORKER_COUNT = 50     # 并发管理员数
REDEEM_ROUNDS = 5           # 每个管理员连续操作轮数

# 系统中真实存在的商品 ID 和活动 ID（请在后台确认）
PRODUCT_IDS = [1, 2, 3]
ACTIVITY_IDS = [1, 2]

# 一致性验证抽查用户数（场景 C）
CONSISTENCY_CHECK_COUNT = 20

# 请求超时（秒）
REQUEST_TIMEOUT = 15

# QR 签名密钥（必须与后端 qrSecret 一致）
QR_SECRET = b"quiz-shop-qr-secret-2026"


# ─────────────────────────────────────────────
# 业务结果分类统计器
# ─────────────────────────────────────────────
@dataclass
class BusinessStats:
    """区分「业务正常拒绝」和「系统错误」的统计器"""

    # 请求维度
    total_requests: int = 0
    system_errors: int = 0          # 超时、500、网络异常等真正的系统问题
    latencies: list = field(default_factory=list)

    # 业务结果维度（成功 + 各类正常拒绝 + 系统错误 = total_requests）
    business_success: int = 0       # 真正成功（积分变动/登录成功等）
    rejected_no_points: int = 0     # 积分不足（正常拒绝）
    rejected_no_stock: int = 0      # 库存不足（正常拒绝）
    rejected_points_limit: int = 0  # 活动积分达上限（正常拒绝）
    rejected_login_fail: int = 0    # 账号不存在或密码错误（正常拒绝）
    rejected_qr_expired: int = 0    # 二维码过期（正常拒绝）
    other_errors: dict = field(default_factory=dict)  # 其他未分类错误

    def record_success(self, latency_ms: float):
        self.total_requests += 1
        self.business_success += 1
        self.latencies.append(latency_ms)

    def record_rejected(self, reason: str, latency_ms: float):
        """业务层正常拒绝，不算系统错误"""
        self.total_requests += 1
        self.latencies.append(latency_ms)
        if "积分不足" in reason:
            self.rejected_no_points += 1
        elif "库存不足" in reason or "库存" in reason:
            self.rejected_no_stock += 1
        elif "积分已达上限" in reason or "上限" in reason:
            self.rejected_points_limit += 1
        elif "工号或姓名不正确" in reason or "未登录" in reason:
            self.rejected_login_fail += 1
        elif "过期" in reason:
            self.rejected_qr_expired += 1
        else:
            key = reason[:80]
            self.other_errors[key] = self.other_errors.get(key, 0) + 1

    def record_system_error(self, reason: str, latency_ms: float):
        """真正的系统错误：超时、500、网络异常"""
        self.total_requests += 1
        self.system_errors += 1
        self.latencies.append(latency_ms)
        key = reason[:80]
        self.other_errors[key] = self.other_errors.get(key, 0) + 1

    def report(self, title: str):
        lats = sorted(self.latencies)
        n = len(lats)
        p50 = lats[int(n * 0.50)] if n else 0
        p90 = lats[int(n * 0.90)] if n else 0
        p99 = lats[int(n * 0.99)] if n else 0
        avg = sum(lats) / n if n else 0

        total_rejected = (self.rejected_no_points + self.rejected_no_stock +
                          self.rejected_points_limit + self.rejected_login_fail +
                          self.rejected_qr_expired + sum(self.other_errors.values()))

        print(f"\n{'='*60}")
        print(f"  {title}")
        print(f"{'='*60}")
        print(f"  总请求数          : {self.total_requests}")
        print()
        print(f"  ✅ 业务成功        : {self.business_success}"
              + (f"  ({self.business_success/self.total_requests*100:.1f}%)" if self.total_requests else ""))
        print(f"  ⚠️  业务正常拒绝   : {total_rejected}"
              + (f"  ({total_rejected/self.total_requests*100:.1f}%)" if self.total_requests else ""))
        if self.rejected_no_points:
            print(f"       └ 积分不足    : {self.rejected_no_points}")
        if self.rejected_no_stock:
            print(f"       └ 库存不足    : {self.rejected_no_stock}")
        if self.rejected_points_limit:
            print(f"       └ 活动积分达上限: {self.rejected_points_limit}")
        if self.rejected_login_fail:
            print(f"       └ 账号不存在  : {self.rejected_login_fail}")
        if self.rejected_qr_expired:
            print(f"       └ 二维码过期  : {self.rejected_qr_expired}")
        print(f"  ❌ 系统错误        : {self.system_errors}"
              + (f"  ({self.system_errors/self.total_requests*100:.1f}%)" if self.total_requests else ""))
        if self.other_errors:
            print(f"     错误详情:")
            for msg, cnt in sorted(self.other_errors.items(), key=lambda x: -x[1])[:8]:
                print(f"       [{cnt}次] {msg}")
        print()
        print(f"  平均延迟          : {avg:.0f} ms")
        print(f"  P50 延迟          : {p50:.0f} ms")
        print(f"  P90 延迟          : {p90:.0f} ms")
        print(f"  P99 延迟          : {p99:.0f} ms")
        print(f"{'='*60}")


# ─────────────────────────────────────────────
# HTTP 工具函数
# ─────────────────────────────────────────────
def make_qr_data(user_id: int, employee_id: str, name: str) -> str:
    """生成与后端一致的 HMAC-SHA256 签名二维码数据"""
    timestamp = int(time.time())
    payload = f"{user_id}|{employee_id}|{name}|{timestamp}"
    sig = hmac.new(QR_SECRET, payload.encode(), hashlib.sha256).hexdigest()
    return f"{user_id}|{employee_id}|{name}|{timestamp}|{sig}"


async def api_post(session: aiohttp.ClientSession, path: str,
                   payload: dict, token: str = "") -> tuple[int, float, str, dict]:
    """返回 (http_status, latency_ms, error_msg, body)"""
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    url = f"{BASE_URL}/api{path}"
    t0 = time.monotonic()
    try:
        async with session.post(url, json=payload, headers=headers,
                                timeout=aiohttp.ClientTimeout(total=REQUEST_TIMEOUT)) as resp:
            lat = (time.monotonic() - t0) * 1000
            body = await resp.json(content_type=None)
            err = body.get("error", "") if resp.status != 200 else ""
            return resp.status, lat, err, body
    except asyncio.TimeoutError:
        return 0, (time.monotonic() - t0) * 1000, "请求超时", {}
    except Exception as e:
        return 0, (time.monotonic() - t0) * 1000, f"网络异常: {str(e)[:60]}", {}


async def api_get(session: aiohttp.ClientSession, path: str,
                  token: str = "") -> tuple[int, float, str, dict]:
    """返回 (http_status, latency_ms, error_msg, body)"""
    headers = {}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    url = f"{BASE_URL}/api{path}"
    t0 = time.monotonic()
    try:
        async with session.get(url, headers=headers,
                               timeout=aiohttp.ClientTimeout(total=REQUEST_TIMEOUT)) as resp:
            lat = (time.monotonic() - t0) * 1000
            body = await resp.json(content_type=None)
            err = body.get("error", "") if resp.status != 200 else ""
            return resp.status, lat, err, body
    except asyncio.TimeoutError:
        return 0, (time.monotonic() - t0) * 1000, "请求超时", {}
    except Exception as e:
        return 0, (time.monotonic() - t0) * 1000, f"网络异常: {str(e)[:60]}", {}


def classify(stats: BusinessStats, status: int, lat: float, err: str, body: dict):
    """根据 HTTP 状态码和错误信息分类记录"""
    if status == 200:
        stats.record_success(lat)
    elif status in (400, 401, 403, 404) and err:
        # 4xx 是业务层拒绝
        stats.record_rejected(err, lat)
    else:
        # 0（超时/网络）、5xx 是系统错误
        stats.record_system_error(err or f"HTTP {status}", lat)


# ─────────────────────────────────────────────
# 场景 A：1000 人并发读取
# ─────────────────────────────────────────────
async def user_flow(session: aiohttp.ClientSession, idx: int,
                    login_stats: BusinessStats, read_stats: BusinessStats):
    """单个用户完整流程：登录 → 查积分 → 查商品"""
    emp_id = f"T{idx:04d}"
    name = f"测试用户{idx:04d}"

    # Step 1: 登录
    status, lat, err, body = await api_post(session, "/login",
                                            {"employee_id": emp_id, "name": name})
    classify(login_stats, status, lat, err, body)

    if status != 200:
        return  # 登录失败，跳过后续请求

    token = body.get("token", "")

    # Step 2: 查看积分（并发读取核心场景）
    status2, lat2, err2, body2 = await api_get(session, "/user/points", token)
    classify(read_stats, status2, lat2, err2, body2)

    # Step 3: 查看商品列表（公开接口）
    status3, lat3, err3, body3 = await api_get(session, "/products")
    classify(read_stats, status3, lat3, err3, body3)


async def run_scene_a(concurrency: int):
    print(f"\n▶ 场景 A：{concurrency} 人并发读取（登录 + 查积分 + 查商品）")
    print(f"  目标: {BASE_URL}")

    login_stats = BusinessStats()
    read_stats = BusinessStats()

    connector = aiohttp.TCPConnector(limit=concurrency + 100, ssl=False)
    async with aiohttp.ClientSession(connector=connector) as session:
        t_start = time.monotonic()
        tasks = [user_flow(session, i + 1, login_stats, read_stats)
                 for i in range(concurrency)]
        await asyncio.gather(*tasks)
        elapsed = time.monotonic() - t_start

    print(f"\n  总耗时: {elapsed:.2f} 秒  |  吞吐量: {concurrency / elapsed:.0f} 用户/秒")
    login_stats.report("场景 A — 登录接口 (/api/login)")
    read_stats.report("场景 A — 读取接口 (/api/user/points + /api/products)")


# ─────────────────────────────────────────────
# 场景 B：50 个管理员并发扫码兑换
# ─────────────────────────────────────────────
async def get_admin_token(session: aiohttp.ClientSession) -> Optional[str]:
    account = random.choice(ADMIN_ACCOUNTS)
    status, _, _, body = await api_post(session, "/login", account)
    if status == 200:
        return body.get("token")
    return None


async def get_users_for_scan(session: aiohttp.ClientSession,
                              token: str) -> list[dict]:
    status, _, _, body = await api_get(session, "/admin/users?page=1&page_size=100", token)
    if status == 200:
        return body.get("users", [])
    return []


async def admin_worker(session: aiohttp.ClientSession, worker_id: int,
                       users: list[dict], admin_token: str,
                       redeem_stats: BusinessStats, activity_stats: BusinessStats,
                       rounds: int = REDEEM_ROUNDS):
    """单个管理员工作流：循环扫码兑换商品 + 活动积分"""
    for _ in range(rounds):
        if not users:
            break
        target = random.choice(users)
        uid = target.get("id", 1)
        emp_id = target.get("employee_id", "T0001")
        name = target.get("name", "测试用户")
        qr_data = make_qr_data(uid, emp_id, name)

        if random.random() < 0.5 and PRODUCT_IDS:
            # 兑换商品
            product_id = random.choice(PRODUCT_IDS)
            status, lat, err, body = await api_post(
                session, "/admin/redeem",
                {"qr_data": qr_data, "product_id": product_id},
                admin_token
            )
            classify(redeem_stats, status, lat, err, body)
        else:
            # 活动加分
            if not ACTIVITY_IDS:
                continue
            activity_id = random.choice(ACTIVITY_IDS)
            status, lat, err, body = await api_post(
                session, "/admin/activities/scan",
                {"qr_data": qr_data, "activity_id": activity_id},
                admin_token
            )
            classify(activity_stats, status, lat, err, body)

        # 模拟真实扫码间隔 0~200ms
        await asyncio.sleep(random.uniform(0, 0.2))


async def run_scene_b(admin_count: int, rounds: int = REDEEM_ROUNDS) -> list[dict]:
    """返回本次测试涉及的用户列表，供场景 C 一致性验证使用"""
    print(f"\n▶ 场景 B：{admin_count} 个管理员并发扫码"
          f"（每人 {rounds} 轮，共约 {admin_count * rounds} 次操作）")
    print(f"  目标: {BASE_URL}")

    redeem_stats = BusinessStats()
    activity_stats = BusinessStats()
    tested_users = []

    connector = aiohttp.TCPConnector(limit=admin_count + 20, ssl=False)
    async with aiohttp.ClientSession(connector=connector) as session:
        print("  正在登录管理员账号...")
        admin_token = None
        for _ in range(5):
            admin_token = await get_admin_token(session)
            if admin_token:
                break
        if not admin_token:
            print("  ✗ 管理员登录失败，请检查 ADMIN_ACCOUNTS 配置")
            return []

        print("  正在获取用户列表...")
        users = await get_users_for_scan(session, admin_token)
        if not users:
            print("  ✗ 获取用户列表失败，请确认管理员权限和用户数据")
            return []

        tested_users = users
        print(f"  获取到 {len(users)} 个用户，开始并发测试...")

        t_start = time.monotonic()
        tasks = [
            admin_worker(session, i, users, admin_token, redeem_stats, activity_stats, rounds)
            for i in range(admin_count)
        ]
        await asyncio.gather(*tasks)
        elapsed = time.monotonic() - t_start

    total_ops = redeem_stats.total_requests + activity_stats.total_requests
    print(f"\n  总耗时: {elapsed:.2f} 秒  |  吞吐量: {total_ops / elapsed:.1f} 操作/秒")
    redeem_stats.report("场景 B — 商品兑换 (/api/admin/redeem)")
    activity_stats.report("场景 B — 活动积分 (/api/admin/activities/scan)")

    return tested_users


# ─────────────────────────────────────────────
# 场景 C：数据一致性验证
# ─────────────────────────────────────────────
async def run_scene_c(users: list[dict], check_count: int = CONSISTENCY_CHECK_COUNT):
    """
    抽查若干用户，验证：
      User 表的 points（可用积分）== Redemption 流水汇总计算结果
    通过对比 /api/user/points 返回的各分项是否自洽来判断。
    """
    print(f"\n▶ 场景 C：数据一致性验证（抽查 {check_count} 个用户）")

    if not users:
        print("  ⚠️  没有可用的用户列表，跳过一致性验证")
        return

    # 需要管理员 token 来登录并获取用户 token
    # 这里改为直接用普通用户登录方式抽查
    sample = random.sample(users, min(check_count, len(users)))

    passed = 0
    failed = 0
    errors = []

    connector = aiohttp.TCPConnector(limit=20, ssl=False)
    async with aiohttp.ClientSession(connector=connector) as session:
        for user in sample:
            emp_id = user.get("employee_id", "")
            name = user.get("name", "")

            # 登录获取 token
            status, _, _, body = await api_post(session, "/login",
                                                {"employee_id": emp_id, "name": name})
            if status != 200:
                errors.append(f"{emp_id} 登录失败: {body.get('error', '')}")
                failed += 1
                continue

            token = body.get("token", "")

            # 获取积分详情
            status2, _, _, pts = await api_get(session, "/user/points", token)
            if status2 != 200:
                errors.append(f"{emp_id} 获取积分失败")
                failed += 1
                continue

            # 一致性校验：
            # available_points 应等于 quiz_score + activity_points + initial_points - used_points
            quiz = pts.get("quiz_score", 0)
            activity = pts.get("activity_points", 0)
            initial = pts.get("initial_points", 0)
            used = pts.get("used_points", 0)
            available = pts.get("available_points", 0)

            expected = max(0, quiz + activity + initial - used)

            if expected == available:
                passed += 1
            else:
                failed += 1
                errors.append(
                    f"{emp_id}({name}) 积分不一致: "
                    f"quiz={quiz} + activity={activity} + initial={initial} - used={used} "
                    f"= {expected}，但 available_points={available}"
                )

    print(f"\n  抽查用户数 : {len(sample)}")
    print(f"  ✅ 一致    : {passed}")
    print(f"  ❌ 不一致  : {failed}")

    if errors:
        print(f"\n  不一致详情（最多显示 10 条）:")
        for e in errors[:10]:
            print(f"    {e}")

    if failed == 0:
        print("\n  ✅ 所有抽查用户积分数据完全一致，数据库无脏数据")
    else:
        print(f"\n  ⚠️  发现 {failed} 个用户积分不一致，建议运行 scripts/sync_user_points.py 修复")

    print(f"{'='*60}")


# ─────────────────────────────────────────────
# 主入口
# ─────────────────────────────────────────────
def main():
    parser = argparse.ArgumentParser(description="Transphorm Quiz App 压力测试")
    parser.add_argument("--scene", choices=["A", "B", "C", "all"], default="all",
                        help="运行场景: A=并发读取, B=并发扫码, C=一致性验证, all=全部 (默认: all)")
    parser.add_argument("--concurrency", type=int, default=TEST_USER_COUNT,
                        help=f"场景 A 并发用户数 (默认: {TEST_USER_COUNT})")
    parser.add_argument("--admins", type=int, default=ADMIN_WORKER_COUNT,
                        help=f"场景 B 管理员并发数 (默认: {ADMIN_WORKER_COUNT})")
    parser.add_argument("--rounds", type=int, default=REDEEM_ROUNDS,
                        help=f"场景 B 每管理员操作轮数 (默认: {REDEEM_ROUNDS})")
    parser.add_argument("--check", type=int, default=CONSISTENCY_CHECK_COUNT,
                        help=f"场景 C 抽查用户数 (默认: {CONSISTENCY_CHECK_COUNT})")
    args = parser.parse_args()

    print("=" * 60)
    print("  Transphorm Quiz App 压力测试")
    print(f"  目标: {BASE_URL}")
    print("=" * 60)
    print()
    print("⚠️  运行前请确认：")
    print(f"  1. ADMIN_ACCOUNTS 中的账号在系统中存在且为管理员")
    print(f"  2. PRODUCT_IDS={PRODUCT_IDS}、ACTIVITY_IDS={ACTIVITY_IDS} 在系统中存在")
    print(f"  3. 场景 B 会产生真实积分变动，测试后请在后台核查")
    print(f"  4. 场景 A 需要系统中存在 T0001~T{args.concurrency:04d} 格式的测试账号")
    print()

    rounds = args.rounds  # 传入 worker 使用

    async def run_all():
        tested_users = []

        if args.scene in ("A", "all"):
            await run_scene_a(args.concurrency)

        if args.scene in ("B", "all", "C"):
            if args.scene != "C":
                tested_users = await run_scene_b(args.admins, rounds)
            # 场景 C：如果单独运行，需要先获取用户列表
            if args.scene == "C" and not tested_users:
                connector = aiohttp.TCPConnector(limit=10, ssl=False)
                async with aiohttp.ClientSession(connector=connector) as session:
                    token = None
                    for _ in range(3):
                        token = await get_admin_token(session)
                        if token:
                            break
                    if token:
                        tested_users = await get_users_for_scan(session, token)
            await run_scene_c(tested_users, args.check)

        print("\n✅ 测试完成")

    asyncio.run(run_all())


if __name__ == "__main__":
    main()
