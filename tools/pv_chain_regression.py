"""PV→Kafka→streamer→ClickHouse 全链路回归测试

确定性设计: 每次运行用随机 User-Agent → udid = MD5(ip, event, bizId, userAgent)
必然全新 → DWD 判 is_new=1, DWM-UV SETNX 成功 → uv=1, 可精确断言, 不受当日去重影响。

用法:
  python tools/pv_chain_regression.py             # 自启 gateway/link/streamer, 测完自动停
  python tools/pv_chain_regression.py --no-start  # 服务已在运行(联调模式), 只触发+断言

断言(每个存量 code):
  新增行 sum(pv) == 3, sum(uv) == 1, max(is_new) == 1
退出码: 0=PASS, 1=FAIL
"""
import argparse
import json
import os
import random
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

import dotenv

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
dotenv.load_dotenv(os.path.join(ROOT, ".env"))

GW = os.environ.get("REGRESSION_GATEWAY", "http://127.0.0.1:8888")
CODES = ["a3z3JYk0", "021Tgota"]  # 在库存量短链(勿删)
HITS_PER_CODE = 3
WINDOW_WAIT = 15  # > DWS 10s 窗口 + Kafka 传递余量

# 沙箱代理会劫持对远程 ClickHouse 的 HTTP 请求, 必须强制直连
os.environ["NO_PROXY"] = "134.175.206.158,localhost,127.0.0.1"
os.environ["no_proxy"] = os.environ["NO_PROXY"]


# ---------------- ClickHouse helpers (HTTP 8123, same as _chcount.py) ----------------

def _ch_conf():
    host = os.environ.get("CLICKHOUSE_ADDR", "localhost:8123").split(":")[0]
    import base64
    auth = base64.b64encode(
        f"{os.environ.get('CLICKHOUSE_USER', 'default')}:{os.environ.get('CLICKHOUSE_PWD', '')}".encode()
    ).decode()
    return host, {"Authorization": f"Basic {auth}"}


def ch_query(sql):
    host, headers = _ch_conf()
    db = os.environ.get("CLICKHOUSE_DB", "qlink_analytics")
    url = f"http://{host}:8123/?database={db}&query=" + urllib.parse.quote(sql)
    req = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(req, timeout=10) as r:
        return r.read().decode().strip()


def ch_baseline(code):
    """返回该 code 当前 max(ts), 无数据返回 0"""
    v = ch_query(f"SELECT max(ts) FROM visit_stats WHERE code = '{code}'")
    return int(v) if v else 0


def ch_delta(code, since_ts):
    """返回 (sum_pv, sum_uv, max_is_new) for rows with ts > since_ts"""
    v = ch_query(
        f"SELECT sum(pv), sum(uv), max(is_new) FROM visit_stats "
        f"WHERE code = '{code}' AND ts > {since_ts}"
    )
    parts = v.split("\t")
    if len(parts) != 3 or parts[0] == "":
        return 0, 0, None
    return int(float(parts[0])), int(float(parts[1])), int(float(parts[2]))


# ---------------- service management ----------------

SERVICES = ["gateway.exe", "link.exe", "streamer.exe"]


def wait_port(port, timeout=20):
    t0 = time.time()
    while time.time() - t0 < timeout:
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=1):
                return True
        except OSError:
            time.sleep(0.5)
    return False


def start_services(log_dir):
    procs = []
    for name in SERVICES:
        log = open(os.path.join(log_dir, name + ".log"), "wb")
        p = subprocess.Popen(
            [os.path.join(ROOT, "bin", name)],
            cwd=ROOT,
            stdout=log,
            stderr=subprocess.STDOUT,
            env={**os.environ, "NO_PROXY": os.environ["NO_PROXY"],
                 "no_proxy": os.environ["NO_PROXY"]},
        )
        procs.append((p, log, name))
    if not wait_port(8888) or not wait_port(8003, timeout=25):
        stop_services(procs)
        raise RuntimeError("gateway(8888)/link(8003) 未在超时内就绪, 查 .regression 日志")
    # streamer 无 HTTP 口, 确认进程未退出即可(消费为拉模式, 会追平积压)
    for p, _, name in procs:
        if p.poll() is not None:
            stop_services(procs)
            raise RuntimeError(f"{name} 启动即退出, 查 .regression 日志")
    return procs


def stop_services(procs):
    for p, log, _ in procs:
        if p.poll() is None:
            p.terminate()
    time.sleep(1.5)
    for p, log, _ in procs:
        if p.poll() is None:
            p.kill()
        log.close()


# ---------------- visit trigger ----------------

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k):
        return None


def trigger_visits(ua):
    """每个 code 打 HITS_PER_CODE 次, 返回 (成功数, 总数)"""
    opener = urllib.request.build_opener(NoRedirect)
    ok = total = 0
    for code in CODES:
        for _ in range(HITS_PER_CODE):
            total += 1
            req = urllib.request.Request(f"{GW}/{code}", headers={"User-Agent": ua})
            try:
                with opener.open(req, timeout=8) as r:
                    st = r.status
            except urllib.error.HTTPError as e:
                st = e.code
            except Exception as e:
                st = repr(e)
            if st in (200, 301, 302, 307, 308):
                ok += 1
            time.sleep(0.4)
    return ok, total


# ---------------- main ----------------

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--no-start", action="store_true", help="服务已运行, 跳过自启/停止")
    args = ap.parse_args()

    ua = f"PvChainRegression/1.0 {random.randint(10**9, 10**10)}"
    print(f"[regression] UA = {ua}")
    print(f"[regression] codes = {CODES}, hits/code = {HITS_PER_CODE}")

    ch_query("SELECT 1")  # CH 连通性预检
    print("[regression] ClickHouse OK")

    baselines = {c: ch_baseline(c) for c in CODES}
    print(f"[regression] baselines(max ts) = {baselines}")

    procs = []
    log_dir = None
    try:
        if not args.no_start:
            log_dir = os.path.join(ROOT, "tools", ".regression")
            os.makedirs(log_dir, exist_ok=True)
            print(f"[regression] starting services (logs: {log_dir}) ...")
            procs = start_services(log_dir)
            print("[regression] services up")
        else:
            if not wait_port(8888, timeout=3) or not wait_port(8003, timeout=3):
                print("[regression] FAIL: gateway/link 未运行 (--no-start 模式)")
                return 1

        print("[regression] triggering visits ...")
        ok, total = trigger_visits(ua)
        print(f"[regression] visits: {ok}/{total} 2xx/3xx")
        if ok != total:
            print("[regression] FAIL: 部分跳转失败")
            return 1

        print(f"[regression] waiting {WINDOW_WAIT}s for DWS window flush ...")
        time.sleep(WINDOW_WAIT)

        failures = []
        print("\n===== assertions =====")
        for c in CODES:
            pv, uv, is_new = ch_delta(c, baselines[c])
            checks = [
                (f"{c} sum(pv) == {HITS_PER_CODE}", pv == HITS_PER_CODE),
                (f"{c} sum(uv) == 1", uv == 1),
                (f"{c} max(is_new) == 1", is_new == 1),
            ]
            for desc, passed in checks:
                print(f"  {'PASS' if passed else 'FAIL'}  {desc}  (actual: pv={pv} uv={uv} is_new={is_new})")
                if not passed:
                    failures.append(desc)

        print("\n===== result =====")
        if failures:
            print(f"REGRESSION FAIL ({len(failures)} assertions)")
            return 1
        print("REGRESSION PASS")
        return 0
    finally:
        if procs:
            stop_services(procs)
            print("[regression] services stopped")


if __name__ == "__main__":
    sys.exit(main())
