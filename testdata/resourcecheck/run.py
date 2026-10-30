"""Reproducible local-only resource measurements; see docs/resource-validation.md.

Run prepare once, then postgres and mysql serially with the same --run-id.
All created Docker resources are stopped and retained; existing resources are untouched.
"""
import argparse
import concurrent.futures as futures
from contextlib import closing, contextmanager
import hashlib
import http.client
import json
import math
import os
from pathlib import Path
import re
import subprocess
import threading
import time
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
PASSWORD = "ResourceTest123!"  # Synthetic fixture only.
DB_PASSWORD = "resource-local-only"
IMAGES = {
    "postgres": "postgres:16-alpine@sha256:4e6e670bb069649261c9c18031f0aded7bb249a5b6664ddec29c013a89310d50",
    "mysql": "mysql:8.4@sha256:d36d39a64cd12a5c1cc9e6aa2bfb5f8d4c81a2f6586e0a04a9ae13939db02209",
}
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


@contextmanager
def measured_response(url, data, headers):
    target = urllib.parse.urlsplit(url)
    with closing(http.client.HTTPConnection(target.hostname, target.port, timeout=20)) as connection:
        connection.connect()
        connection.auto_open = False
        connection.putrequest("POST" if data is not None else "GET",
                              target.path + ("?" + target.query if target.query else ""))
        for name, value in headers.items():
            connection.putheader(name, value)
        if data is not None:
            connection.putheader("Content-Length", str(len(data)))
        connection.endheaders()
        # Read concurrently with writes, as Go/browser HTTP clients do. Reading
        # only after sendall fails can already be too late on Windows TCP abort.
        def send_body():
            try:
                connection.send(data)
            except (OSError, http.client.HTTPException):
                pass  # Only a complete observed HTTP response can classify this request.

        writer = threading.Thread(target=send_body) if data else None
        if writer:
            writer.start()
        try:
            with connection.getresponse() as response:
                yield response
        finally:
            connection.close()
            if writer:
                writer.join()


def command(*args, env=None, combined=False):
    result = subprocess.run(args, cwd=ROOT, env=env, capture_output=True, text=True, encoding="utf-8")
    if result.returncode:
        raise RuntimeError(f"{args[0:3]} failed: {result.stderr.strip()}")
    return (result.stdout + (result.stderr if combined else "")).strip()


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def stamp():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def prepare(base, image):
    base.mkdir()
    build = base / "image"
    build.mkdir()
    helper = base / "resourcecheck.exe"
    command("go", "build", "-trimpath", "-o", str(helper), "./testdata/resourcecheck")
    fixture = json.loads(command(str(helper), "generate", str(base / "fixture")))
    write_json(base / "fixture-summary.json", fixture)
    env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
    command("go", "build", "-trimpath", "-o", str(build / "wishingskin"), "./cmd/wishingskin", env=env)
    command("go", "build", "-trimpath", "-o", str(build / "resourcecheck"), "./testdata/resourcecheck", env=env)
    (build / "Dockerfile").write_text("FROM wishingskin:phase3\nCOPY wishingskin resourcecheck /usr/local/bin/\n", encoding="utf-8")
    build_log = command("docker", "build", "--pull=false", "-t", image, str(build), combined=True)
    (base / "build.log").write_text(build_log, encoding="utf-8")
    info = json.loads(command("docker", "info", "--format", "{{json .}}"))
    write_json(base / "environment.json", {
        "started_utc": stamp(), "go": command("go", "version"),
        "docker": {key: info[key] for key in ["ServerVersion", "NCPU", "MemTotal", "CgroupVersion", "OSType"]},
        "base_image": command("docker", "image", "inspect", "wishingskin:phase3", "--format", "{{.Id}}"),
        "image": command("docker", "image", "inspect", image, "--format", "{{.Id}}"),
        "binary_sha256": hashlib.sha256((build / "wishingskin").read_bytes()).hexdigest(),
        "frontend_sha256": {str(p.relative_to(ROOT / "web/dist")): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted((ROOT / "web/dist").rglob("*")) if p.is_file()},
        "database_images": IMAGES,
    })
    print(json.dumps(fixture), flush=True)


class Sampler:
    def __init__(self, path, names):
        self.path, self.names = path, names
        self.stop = threading.Event()
        self.phase = "startup"
        self.rows, self.errors = [], []
        self.lock = threading.Lock()
        self.thread = threading.Thread(target=self.sample_loop)

    def sample(self):
        with self.lock:
            self.sample_locked()

    def sample_locked(self):
        phase = self.phase
        for role, name in self.names.items():
            raw = command("docker", "exec", name, "cat", *["/sys/fs/cgroup/" + p for p in ["memory.current", "memory.peak", "memory.events", "cpu.stat"]])
            lines = raw.splitlines()
            values = {"memory_current": int(lines[0]), "memory_peak": int(lines[1])}
            values.update({key: int(value) for key, value in (line.split() for line in lines[2:])})
            row = dict(utc=stamp(), monotonic=time.monotonic(), phase=phase, role=role, **values)
            self.rows.append(row)
            with self.path.open("a", encoding="utf-8") as out:
                out.write(json.dumps(row) + "\n")

    def sample_loop(self):
        while not self.stop.is_set():
            start = time.monotonic()
            try:
                self.sample()
            except Exception as exc:
                self.errors.append(str(exc))
            self.stop.wait(max(0, 1 - (time.monotonic() - start)))

    def start(self):
        self.thread.start()

    def close(self):
        self.stop.set()
        self.thread.join()
        try:
            self.sample()
        except Exception as exc:
            self.errors.append(str(exc))


def percentiles(values):
    values = sorted(values)
    return {key: round(values[max(0, math.ceil(len(values) * q) - 1)], 3) for key, q in [("p50_ms", .5), ("p95_ms", .95), ("p99_ms", .99), ("max_ms", 1)]} if values else {}


class Load:
    def __init__(self, out, fixture):
        self.out, self.fixture = out, fixture
        self.origin = "http://localhost:18096"
        self.sessions = {}
        self.upload = (fixture / "upload.png").read_bytes()
        snapshot = json.loads((fixture / "snapshot.json").read_text())
        self.hashes = [t["hash"] for t in snapshot["textures"][::2]]
        self.phases = []

    def request(self, kind, path, *, payload=None, data=None, headers=None, ip=1, user=None, allowed=()):
        start = time.monotonic()
        hdr = {"Origin": self.origin, "X-Forwarded-For": f"198.18.{ip // 250 + 1}.{ip % 250 + 1}"}
        if payload is not None:
            data = json.dumps(payload).encode()
            hdr["Content-Type"] = "application/json"
        hdr.update(headers or {})
        result = dict(kind=kind, status=0, outcome="unexpected", error="", unexpected=True, started=start)
        try:
            with measured_response(self.origin + path, data, hdr) as response:
                body = response.read()
                result["status"], result["bytes"] = response.status, len(body)
                parsed = json.loads(body) if response.headers.get("Content-Type", "").startswith("application/json") else None
                result["error"] = parsed.get("error", "") if isinstance(parsed, dict) else ""
                success = response.status == (201 if kind == "upload" else 200)
                if success and kind == "login":
                    success = parsed.get("user", {}).get("id") == user and bool(parsed.get("csrfToken"))
                    cookie = response.headers.get("Set-Cookie", "").split(";", 1)[0]
                    success = success and cookie.startswith("ws_session=")
                    if success and user <= 4:
                        self.sessions[user] = {"Cookie": cookie, "X-CSRF-Token": parsed["csrfToken"]}
                elif success and kind == "library":
                    success = isinstance(parsed.get("textures"), list) and parsed.get("total", 0) > 0
                elif success and kind == "png":
                    success = hashlib.sha256(body).hexdigest() == path.rsplit("/", 1)[1]
                elif success and kind == "site":
                    success = bool(parsed.get("name"))
                elif success and kind == "upload":
                    success = parsed.get("fileHash") == hashlib.sha256(self.upload).hexdigest()
                if success:
                    result["outcome"], result["unexpected"] = "success", False
                else:
                    classification = {
                        (429, "登录繁忙，请稍后重试"): "auth_busy",
                        (429, "登录尝试过于频繁，请稍后重试"): "auth_window",
                        (429, "正在处理其他上传，请稍后重试"): "upload_busy",
                        (429, "上传请求过于频繁，请一分钟后重试"): "upload_window",
                        (429, "邮件处理繁忙，请稍后重试"): "mail_busy",
                        (503, "暂时无法发送邮件"): "mail_unavailable",
                    }.get((response.status, result["error"]))
                    if classification in allowed:
                        result["outcome"], result["unexpected"] = classification, False
                    elif not result["error"]:
                        result["error"] = "response content/status mismatch"
        except Exception as exc:
            result["error"] = type(exc).__name__
        result["ended"] = time.monotonic()
        result["ms"] = (result["ended"] - start) * 1000
        return result

    def login(self, user, ip, allowed=()):
        return self.request("login", "/api/auth/login", payload={"email": f"resource{user:04d}@example.invalid", "password": PASSWORD}, user=user, ip=ip, allowed=allowed)

    def mail(self, user):
        return self.request("mail", "/api/auth/email", payload={"email": f"resource{user:04d}@example.invalid", "purpose": "reset"}, ip=user, allowed=("mail_busy", "mail_unavailable"))

    def browse(self, i):
        paths = ["/api/library?pageSize=24&page=" + str(i % 40 + 1), "/api/library?sort=collections", "/api/library?q=Resource%20skin%2000", "/api/library?owner=" + str(i % 1000 + 1), "/textures/" + self.hashes[i % 1000], "/api/site"]
        kind = ["library", "library", "library", "library", "png", "site"][i % 6]
        return self.request(kind, paths[i % 6])

    def upload_one(self, user, allowed=()):
        boundary = "wishing-resource-boundary"
        body = bytearray()
        for key, value in {"kind": "skin", "name": "Resource maximum pixels", "model": "slim", "isPublic": "false", "expectedCost": "0"}.items():
            body.extend(f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n'.encode())
        body.extend(f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="resource.png"\r\nContent-Type: image/png\r\n\r\n'.encode())
        body.extend(self.upload)
        body.extend(f"\r\n--{boundary}--\r\n".encode())
        return self.request("upload", "/api/textures", data=bytes(body), headers=dict(self.sessions[user], **{"Content-Type": f"multipart/form-data; boundary={boundary}"}), ip=5000 + user, allowed=allowed)

    def phase(self, name, count, concurrency, action, rate=0, duration=0):
        print(f"{stamp()} {name}", flush=True)
        start, pending, rows, delay = time.monotonic(), set(), [], 0
        with futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
            for i in range(count):
                due = start + i / rate if rate else start
                if rate:
                    time.sleep(max(0, due - time.monotonic()))
                if len(pending) >= concurrency:
                    done, pending = futures.wait(pending, return_when=futures.FIRST_COMPLETED)
                    rows.extend(f.result() for f in done)
                delay = max(delay, time.monotonic() - due) if rate else 0
                pending.add(pool.submit(action, i))
            rows.extend(f.result() for f in futures.as_completed(pending))
        time.sleep(max(0, start + duration - time.monotonic()))
        return self.record(name, rows, start, concurrency, rate, delay)

    def record(self, name, rows, start, concurrency, rate=0, delay=0):
        elapsed = time.monotonic() - start
        with (self.out / "requests.jsonl").open("a", encoding="utf-8") as file:
            for row in sorted(rows, key=lambda row: row["started"]):
                file.write(json.dumps(dict(phase=name, **row), ensure_ascii=False) + "\n")
        outcomes = sorted({r["outcome"] for r in rows})
        summary = dict(phase=name, seconds=elapsed, concurrency=concurrency, target_rps=rate,
                       actual_rps=len(rows) / elapsed, max_dispatch_delay_ms=delay * 1000,
                       requests=len(rows), unexpected=sum(r["unexpected"] for r in rows),
                       status_counts={str(s): sum(r["status"] == s for r in rows) for s in sorted({r["status"] for r in rows})},
                       outcomes={key: dict(count=sum(r["outcome"] == key for r in rows), **percentiles([r["ms"] for r in rows if r["outcome"] == key])) for key in outcomes})
        self.phases.append(summary)
        write_json(self.out / "http-summary.json", self.phases)
        return rows

    def delayed_mail(self):
        name, start = "mail-delay", time.monotonic()
        print(f"{stamp()} {name}", flush=True)
        with futures.ThreadPoolExecutor(max_workers=6) as pool:
            waiting = [pool.submit(self.mail, i) for i in range(990, 996)]
            deadline = time.monotonic() + 4
            state = {}
            while time.monotonic() < deadline:
                state = json.loads(OPENER.open("http://localhost:18097/state", timeout=2).read())
                if state["active"] == 2:
                    break
                time.sleep(.1)
            logins = []
            for i in range(8):
                logins.append(self.login(800 + i, 800 + i))
                time.sleep(.25)
            mail = [f.result() for f in waiting]
        final = json.loads(OPENER.open("http://localhost:18097/state", timeout=2).read())
        timeouts = [r for r in mail if r["outcome"] == "mail_unavailable"]
        checks = dict(barrier_active=state.get("active"), smtp=final,
                      logins_before_timeout=bool(timeouts) and max(r["ended"] for r in logins) < min(r["ended"] for r in timeouts),
                      timeout_count=len(timeouts), busy_count=sum(r["outcome"] == "mail_busy" for r in mail),
                      timeout_in_range=all(7500 <= r["ms"] <= 10000 for r in timeouts),
                      all_logins_success=all(r["outcome"] == "success" for r in logins))
        write_json(self.out / "mail-delay-checks.json", checks)
        self.record(name, mail + logins, start, 7)
        return checks


def resource_summary(rows):
    output = {}
    for phase in sorted({r["phase"] for r in rows}):
        group = [r for r in rows if r["phase"] == phase]
        output[phase] = {}
        for role in ["app", "db"]:
            values = [r for r in group if r["role"] == role]
            if values:
                output[phase][role] = dict(samples=len(values), sampled_peak_bytes=max(r["memory_current"] for r in values), lifecycle_peak_bytes=max(r["memory_peak"] for r in values),
                                          cpu_seconds=(values[-1]["usage_usec"] - values[0]["usage_usec"]) / 1e6,
                                          oom_kill=max(r["oom_kill"] for r in values), oom=max(r["oom"] for r in values))
    return output


def run_database(driver, base, image, run_id):
    out = base / driver
    out.mkdir()
    prefix = f"ws-resource-{run_id}-{driver}"
    network, db, importer, app, smtp = [prefix + "-" + suffix for suffix in ["net", "db", "import", "app", "smtp"]]
    containers, collector = [], None
    command("docker", "network", "create", network)
    try:
        gateway = json.loads(command("docker", "network", "inspect", network))[0]["IPAM"]["Config"][0]["Gateway"]
        db_args = ["docker", "run", "-d", "--name", db, "--network", network, "--network-alias", "db", "--cpus=1", "--memory=1536m", "--memory-swap=1536m", "--mount", f"type=volume,src={prefix}-db,dst=" + ("/var/lib/mysql" if driver == "mysql" else "/var/lib/postgresql/data")]
        if driver == "mysql":
            db_args += ["-e", "MYSQL_DATABASE=resource", "-e", "MYSQL_ROOT_PASSWORD=" + DB_PASSWORD]
            ready = ["docker", "exec", "-e", "MYSQL_PWD=" + DB_PASSWORD, db, "mysql", "-h127.0.0.1", "-uroot", "-D", "resource", "-Nse", "SELECT 1"]
            dsn = f"root:{DB_PASSWORD}@tcp(db:3306)/resource?parseTime=true"
        else:
            db_args += ["-e", "POSTGRES_DB=resource", "-e", "POSTGRES_USER=resource", "-e", "POSTGRES_PASSWORD=" + DB_PASSWORD]
            ready = ["docker", "exec", db, "psql", "-U", "resource", "-d", "resource", "-Atc", "SELECT 1"]
            dsn = f"postgres://resource:{DB_PASSWORD}@db:5432/resource?sslmode=disable"
        command(*db_args, IMAGES[driver])
        containers.append(db)
        for _ in range(120):
            try:
                if command(*ready) == "1":
                    break
            except RuntimeError:
                pass
            time.sleep(1)
        else:
            raise RuntimeError("isolated database did not become ready")
        common = ["--network", network, "--cpus=1", "--memory=512m", "--memory-swap=512m", "--no-healthcheck",
                  "--mount", f"type=volume,src={prefix}-data,dst=/data", "-e", "WS_DATABASE_DRIVER=" + driver,
                  "-e", "WS_DATABASE_DSN=" + dsn, "-e", "WS_PUBLIC_URL=http://localhost:18096"]
        command("docker", "run", "-d", "--name", importer, *common, "--mount", f"type=bind,src={base / 'fixture'},dst=/fixture,readonly", "--entrypoint", "resourcecheck", image, "import")
        containers.append(importer)
        collector = Sampler(out / "resources.jsonl", {"app": importer, "db": db})
        collector.phase = "import"
        collector.start()
        for _ in range(180):
            try:
                result = json.loads(command("docker", "exec", importer, "cat", "/data/import-result.json"))
                break
            except RuntimeError:
                pass
            time.sleep(1)
        else:
            raise RuntimeError("import did not complete within 180s")
        report = json.loads(command("docker", "exec", importer, "cat", "/data/import-report.json"))
        write_json(out / "import.json", dict(result=result, report=report))
        if not result["success"] or not report["imported"]:
            raise RuntimeError("import failed")
        collector.close()
        samples, sample_errors = collector.rows, collector.errors
        collector = None
        command("docker", "stop", "-t", "10", importer)
        command("docker", "run", "-d", "--name", app, *common, "-p", "127.0.0.1:18096:8090", "-p", "127.0.0.1:18097:2526",
                "-e", "WS_TRUSTED_PROXIES=" + gateway + "/32", "-e", "WS_SMTP_ADDRESS=127.0.0.1:2525", "-e", "WS_SMTP_FROM=resource@example.invalid", "-e", "WS_SMTP_TLS=local", image)
        containers.append(app)
        for _ in range(30):
            try:
                if json.loads(OPENER.open("http://localhost:18096/readyz", timeout=2).read())["status"] == "ready":
                    break
            except (OSError, ValueError):
                pass
            time.sleep(1)
        else:
            raise RuntimeError("application did not become ready")
        collector = Sampler(out / "resources.jsonl", {"app": app, "db": db})
        collector.phase = "idle"
        collector.start()
        print(f"{stamp()} {driver} idle", flush=True)
        time.sleep(30)
        load = Load(out, base / "fixture")
        for name, count, concurrency, action, rate, duration in [
            ("browse", 1200, 8, load.browse, 20, 60),
            ("auth-steady", 120, 2, lambda i: load.login(i + 1, i + 1), 2, 60),
            ("auth-burst", 400, 16, lambda i: load.login(i + 200, i + 200, ("auth_busy",)), 40, 10),
            ("auth-ip-window", 20, 2, lambda i: load.login(i + 700, 9000, ("auth_window",)), 0, 0),
            ("upload-steady", 4, 1, lambda i: load.upload_one(i + 1), 0, 0),
            ("upload-burst", 32, 8, lambda i: load.upload_one(1, ("upload_busy", "upload_window")), 0, 0),
        ]:
            collector.phase = name
            load.phase(name, count, concurrency, action, rate, duration)
            collector.sample()
        command("docker", "run", "-d", "--name", smtp, "--network", "container:" + app, "--cpus=.1", "--memory=32m", "--memory-swap=32m", "--no-healthcheck", "--entrypoint", "resourcecheck", image, "smtp")
        containers.append(smtp)
        for _ in range(20):
            try:
                OPENER.open("http://localhost:18097/state", timeout=2).close()
                break
            except OSError:
                time.sleep(.1)
        collector.phase = "mail-delay"
        mail_checks = load.delayed_mail()
        collector.sample()
        command("docker", "stop", "-t", "2", smtp)
        collector.phase = "mail-unavailable"
        load.phase("mail-unavailable", 4, 2, lambda i: load.mail(980 + i) if i < 2 else load.login(850 + i, 850 + i))
        collector.phase = "upload-download"
        load.phase("upload-download", 1, 1, lambda _: load.request("png", "/textures/" + hashlib.sha256(load.upload).hexdigest()))
        collector.close()
        samples += collector.rows
        sample_errors += collector.errors
        collector = None
        write_json(out / "resource-summary.json", resource_summary(samples))
        # Query only this new fixture database. No source or preview database is used.
        counts_query = "SELECT 'users',COUNT(*) FROM users UNION ALL SELECT 'characters',COUNT(*) FROM characters UNION ALL SELECT 'textures',COUNT(*) FROM textures UNION ALL SELECT 'records',COUNT(*) FROM texture_records UNION ALL SELECT 'wardrobe',COUNT(*) FROM wardrobe"
        counts = command(*(ready[:-1] + [counts_query]))
        (out / "counts.txt").write_text(counts + "\n", encoding="utf-8")
        counts_map = {key: int(value) for key, value in (re.split(r"[|\t]", line) for line in counts.splitlines())}
        state = {}
        for role, name in {"app": app, "db": db, "import": importer}.items():
            obj = json.loads(command("docker", "inspect", name))[0]
            state[role] = {"id": obj["Id"], "oom_killed": obj["State"]["OOMKilled"], "restarts": obj["RestartCount"], "running": obj["State"]["Running"], "memory_limit": obj["HostConfig"]["Memory"], "memory_swap": obj["HostConfig"]["MemorySwap"], "nano_cpus": obj["HostConfig"]["NanoCpus"]}
        version = command(*(ready[:-1] + ["SELECT version() "]))
        ready_final = json.loads(OPENER.open("http://localhost:18096/readyz", timeout=2).read())
        upload_success = sum(p["outcomes"].get("success", {}).get("count", 0) for p in load.phases if p["phase"] in ["upload-steady", "upload-burst"])
        expected_counts = dict(users=1000, characters=1000, textures=1000 + bool(upload_success), records=2000 + upload_success, wardrobe=1000)
        ip_phase = next(p for p in load.phases if p["phase"] == "auth-ip-window")
        checks = dict(
            no_unexpected_http=all(p["unexpected"] == 0 for p in load.phases),
            counts_match=counts_map == expected_counts,
            ip_window=ip_phase["outcomes"].get("success", {}).get("count") == 10 and ip_phase["outcomes"].get("auth_window", {}).get("count") == 10,
            mail_isolation=mail_checks["barrier_active"] == 2 and mail_checks["smtp"]["peak"] == 2 and mail_checks["timeout_count"] == 2 and mail_checks["busy_count"] == 4 and mail_checks["timeout_in_range"] and mail_checks["all_logins_success"] and mail_checks["logins_before_timeout"],
            no_oom=all(r["oom"] == 0 and r["oom_kill"] == 0 for r in samples) and all(not s["oom_killed"] for s in state.values()),
            no_restarts=all(s["restarts"] == 0 for s in state.values()) and state["app"]["running"] and state["db"]["running"],
            sampler_complete=not sample_errors, ready=ready_final.get("status") == "ready")
        write_json(out / "final.json", dict(driver=driver, version=version, finished_utc=stamp(), state=state, ready=ready_final, sampler_errors=sample_errors, gateway=gateway,
            checks=checks, status="PASS" if all(checks.values()) else "FAIL", counts=counts_map, expected_counts=expected_counts,
            http_unexpected=sum(p["unexpected"] for p in load.phases), mail_checks=mail_checks,
            oom_kills=max(r["oom_kill"] for r in samples)))
        print(f"{driver}: measurements saved in {out}", flush=True)
    finally:
        if collector is not None:
            collector.close()
        for name in reversed(containers):
            command("docker", "stop", "-t", "10", name)
            logs = command("docker", "logs", name, combined=True)
            (out / (name.rsplit("-", 1)[1] + ".log")).write_text(logs, encoding="utf-8")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["prepare", "postgres", "mysql"])
    parser.add_argument("--run-id", required=True)
    args = parser.parse_args()
    if not re.fullmatch(r"[a-z0-9-]{1,30}", args.run_id):
        parser.error("run-id must be 1-30 lowercase letters, digits or hyphens")
    os.chdir(ROOT)
    base = ROOT / "artifacts" / ("resource-" + args.run_id)
    image = "wishingskin:resource-" + args.run_id
    if args.action == "prepare":
        prepare(base, image)
    else:
        run_database(args.action, base, image, args.run_id)
