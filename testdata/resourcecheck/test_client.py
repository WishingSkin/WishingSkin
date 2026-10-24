"""Check that large-body early rejection remains an observed HTTP response."""
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import threading
import unittest

from run import Load


class EarlyResponseTest(unittest.TestCase):
    def test_connection_without_response_stays_unexpected(self):
        class NoResponse(BaseHTTPRequestHandler):
            def do_POST(self):
                self.close_connection = True

            def log_message(self, *args):
                pass

        with ThreadingHTTPServer(("127.0.0.1", 0), NoResponse) as server:
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                load = Load.__new__(Load)
                load.origin = f"http://127.0.0.1:{server.server_port}"
                result = load.request("upload", "/closed", data=b"x" * (4 * 1024 * 1024), allowed=("upload_window",))
                self.assertEqual(result["status"], 0, result)
                self.assertTrue(result["unexpected"], result)
            finally:
                server.shutdown()
                thread.join()

    def test_large_upload_reads_early_429(self):
        class Early429(BaseHTTPRequestHandler):
            def do_POST(self):
                body = json.dumps({"error": "上传请求过于频繁，请一分钟后重试"}, ensure_ascii=False).encode()
                self.send_response(429)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.send_header("Connection", "close")
                self.end_headers()
                self.wfile.write(body)
                self.wfile.flush()
                self.close_connection = True

            def log_message(self, *args):
                pass

        with ThreadingHTTPServer(("127.0.0.1", 0), Early429) as server:
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                load = Load.__new__(Load)
                load.origin = f"http://127.0.0.1:{server.server_port}"
                for _ in range(10):
                    result = load.request("upload", "/early", data=b"x" * (4 * 1024 * 1024), allowed=("upload_window",))
                    self.assertEqual(result["status"], 429, result)
                    self.assertEqual(result["outcome"], "upload_window", result)
                    self.assertFalse(result["unexpected"], result)
            finally:
                server.shutdown()
                thread.join()


if __name__ == "__main__":
    unittest.main()
