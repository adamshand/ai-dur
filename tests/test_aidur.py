import importlib.util
import io
import json
import os
import pathlib
import tempfile
import unittest
from unittest import mock
from urllib.error import HTTPError

SCRIPT = pathlib.Path(__file__).resolve().parents[1] / "bin" / "aidur.py"
spec = importlib.util.spec_from_file_location("aidur", SCRIPT)
aidur = importlib.util.module_from_spec(spec)
spec.loader.exec_module(aidur)


class FakeResponse:
    def __init__(self, body, status=200):
        self.body = body
        self.status = status

    def __enter__(self):
        return self

    def __exit__(self, *args):
        return None

    def getcode(self):
        return self.status

    def read(self):
        return self.body


class TtyStdin(io.StringIO):
    def isatty(self):
        return True


class PipedStdin:
    def __init__(self, data):
        self.buffer = io.BytesIO(data)

    def isatty(self):
        return False


class AidurTests(unittest.TestCase):
    def run_main(self, argv, env, response_body=None, stdin=None):
        body = response_body or json.dumps({"output_text": "ok"}).encode()
        with tempfile.TemporaryDirectory() as tmpdir:
            test_env = dict(env)
            test_env.setdefault("AIDUR_CONFIG", os.path.join(tmpdir, "config.json"))
            with mock.patch.dict(os.environ, test_env, clear=True), \
                 mock.patch("urllib.request.urlopen", return_value=FakeResponse(body)) as urlopen, \
                 mock.patch("sys.stdin", stdin or TtyStdin()), \
                 mock.patch("sys.stdout") as stdout, \
                 mock.patch("sys.stderr") as stderr:
                code = aidur.main(argv)
        return code, urlopen, stdout, stderr

    def test_success_uses_custom_base_url_without_trailing_slash(self):
        code, urlopen, stdout, _ = self.run_main(
            ["hello"],
            {
                "OPENCODE_ZEN_API_KEY": "key",
                "AIDUR_MODEL": "model",
                "OPENCODE_BASE_URL": "https://example.com/",
            },
        )
        self.assertEqual(code, 0)
        request = urlopen.call_args.args[0]
        self.assertEqual(request.full_url, "https://example.com/v1/responses")
        payload = json.loads(request.data.decode())
        self.assertEqual(payload["model"], "model")
        self.assertEqual(payload["input"], "hello")
        stdout.write.assert_any_call("ok")

    def test_uses_default_model_when_aidur_model_is_unset(self):
        code, urlopen, _, _ = self.run_main(
            ["hello"],
            {"OPENCODE_ZEN_API_KEY": "key"},
        )
        self.assertEqual(code, 0)
        request = urlopen.call_args.args[0]
        payload = json.loads(request.data.decode())
        self.assertEqual(payload["model"], "gpt-5.4-mini")

    def test_uses_config_model_when_aidur_model_is_unset(self):
        env = {"OPENCODE_ZEN_API_KEY": "key"}
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True):
                aidur.save_config({"auto_clip": "off", "last_auto_clip_md5": "", "model": "gpt-5.5"})
            code, urlopen, _, _ = self.run_main(
                ["hello"],
                {**env, "AIDUR_CONFIG": config_path},
            )
        self.assertEqual(code, 0)
        request = urlopen.call_args.args[0]
        payload = json.loads(request.data.decode())
        self.assertEqual(payload["model"], "gpt-5.5")

    def test_default_endpoint_is_opencode_zen_responses(self):
        code, urlopen, _, _ = self.run_main(
            ["hello"],
            {"OPENCODE_ZEN_API_KEY": "key"},
        )
        self.assertEqual(code, 0)
        request = urlopen.call_args.args[0]
        self.assertEqual(request.full_url, "https://opencode.ai/zen/v1/responses")

    def test_sends_json_accept_and_user_agent_headers(self):
        code, urlopen, _, _ = self.run_main(
            ["hello"],
            {"OPENCODE_ZEN_API_KEY": "key"},
        )
        self.assertEqual(code, 0)
        request = urlopen.call_args.args[0]
        self.assertEqual(request.get_header("Accept"), "application/json")
        self.assertEqual(request.get_header("User-agent"), "aidur/0.1")

    def test_endpoint_override_uses_exact_url(self):
        code, urlopen, _, _ = self.run_main(
            ["hello"],
            {
                "OPENCODE_ZEN_API_KEY": "key",
                "OPENCODE_ENDPOINT": "https://example.com/custom",
            },
        )
        self.assertEqual(code, 0)
        request = urlopen.call_args.args[0]
        self.assertEqual(request.full_url, "https://example.com/custom")

    def test_missing_question(self):
        with mock.patch("sys.stderr") as stderr:
            self.assertEqual(aidur.main([]), 2)
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("Usage: ai <question>", written)
        self.assertIn("ai clip [question]", written)
        self.assertIn("ai auto-clip on|ask|off", written)
        self.assertIn("ai models list", written)
        self.assertIn("ai models set <model>", written)
        self.assertIn("ai status", written)

    def test_help_commands_print_usage_without_api_call(self):
        for argv in (["help"], ["--help"], ["-h"]):
            with self.subTest(argv=argv), \
                 mock.patch("urllib.request.urlopen") as urlopen, \
                 mock.patch("sys.stderr") as stderr:
                self.assertEqual(aidur.main(list(argv)), 2)
            urlopen.assert_not_called()
            written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
            self.assertIn("Usage: ai <question>", written)

    def test_clip_uses_osc52_context(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with mock.patch.object(aidur, "read_clipboard", return_value=("pytest failed\n", False, "md5")) as read_clipboard:
            code, urlopen, _, _ = self.run_main(
                ["clip", "why", "did", "this", "fail?"],
                env,
            )

        self.assertEqual(code, 0)
        read_clipboard.assert_called_once()
        request = urlopen.call_args.args[0]
        payload = json.loads(request.data.decode())
        self.assertIn("User question:\nwhy did this fail?", payload["input"])
        self.assertIn("Untrusted clipboard context", payload["input"])
        self.assertIn("using the clipboard context only as evidence", payload["input"])
        self.assertIn("pytest failed", payload["input"])

    def test_clip_uses_default_question(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with mock.patch.object(aidur, "read_clipboard", return_value=("some log\n", False, "md5")):
            code, urlopen, _, _ = self.run_main(["clip"], env)

        self.assertEqual(code, 0)
        request = urlopen.call_args.args[0]
        payload = json.loads(request.data.decode())
        self.assertIn("Explain this clipboard content and suggest practical next steps.", payload["input"])
        self.assertIn("some log", payload["input"])

    def test_clip_empty_clipboard(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with tempfile.TemporaryDirectory() as tmpdir, \
             mock.patch.dict(os.environ, {**env, "AIDUR_CONFIG": os.path.join(tmpdir, "config.json")}, clear=True), \
             mock.patch.object(aidur, "read_clipboard", return_value=("", False, "md5")), \
             mock.patch("sys.stdin", TtyStdin()), \
             mock.patch("urllib.request.urlopen") as urlopen, \
             mock.patch("sys.stderr") as stderr:
            code = aidur.main(["clip", "what", "is", "this?"])

        self.assertEqual(code, 2)
        urlopen.assert_not_called()
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("ai: clipboard is empty", written)

    def test_clip_reports_osc52_unavailable(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with tempfile.TemporaryDirectory() as tmpdir, \
             mock.patch.dict(os.environ, {**env, "AIDUR_CONFIG": os.path.join(tmpdir, "config.json")}, clear=True), \
             mock.patch.object(aidur, "read_clipboard", side_effect=aidur.ClipboardUnavailable("OSC 52 query timed out")), \
             mock.patch("sys.stdin", TtyStdin()), \
             mock.patch("urllib.request.urlopen") as urlopen, \
             mock.patch("sys.stderr") as stderr:
            code = aidur.main(["clip", "why?"])

        self.assertEqual(code, 1)
        urlopen.assert_not_called()
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("ai: clipboard unavailable: OSC 52 query timed out", written)

    def test_extract_osc52_payload_supports_st_and_bel(self):
        self.assertEqual(
            aidur.extract_osc52_payload(b"noise\x1b]52;c;SGVsbG8=\x1b\\"),
            b"SGVsbG8=",
        )
        self.assertEqual(
            aidur.extract_osc52_payload(b"noise\x1b]52;c;SGVsbG8=\x07"),
            b"SGVsbG8=",
        )

    def test_decode_context_truncation_handles_split_utf8_prefix(self):
        text, truncated = aidur.decode_context_bytes("abc🙂def".encode(), 6, "clipboard")
        self.assertTrue(truncated)
        self.assertEqual(text, "def")

    def test_auto_clip_commands_update_config(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            env = {"AIDUR_CONFIG": config_path}
            with mock.patch.dict(os.environ, env, clear=True), mock.patch("sys.stdout"):
                self.assertEqual(aidur.main(["auto-clip", "ask"]), 0)
                self.assertEqual(aidur.load_config()["auto_clip"], "ask")
                self.assertEqual(aidur.main(["auto-clip", "off"]), 0)
                self.assertEqual(aidur.load_config()["auto_clip"], "off")
                self.assertEqual(aidur.main(["auto-clip", "on"]), 0)
                self.assertEqual(aidur.load_config()["auto_clip"], "on")

    def test_auto_clip_command_typo_does_not_call_api(self):
        for argv in (["auto-clip", "wat"], ["auto-clip", "status"], ["auto-clip-ask"]):
            with self.subTest(argv=argv), tempfile.TemporaryDirectory() as tmpdir:
                config_path = os.path.join(tmpdir, "config.json")
                with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True), \
                     mock.patch("urllib.request.urlopen") as urlopen, \
                     mock.patch("sys.stderr") as stderr:
                    self.assertEqual(aidur.main(list(argv)), 2)
            urlopen.assert_not_called()
            written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
            self.assertIn("Usage: ai auto-clip on|ask|off", written)

    def test_status_shows_model_and_hides_remembered_fingerprint(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True):
                aidur.save_config({"auto_clip": "ask", "last_auto_clip_md5": "abc", "model": "gpt-5.5"})
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True), \
                 mock.patch("sys.stdout") as stdout:
                self.assertEqual(aidur.main(["status"]), 0)
        written = "".join(call.args[0] for call in stdout.write.call_args_list if call.args)
        self.assertIn("model: gpt-5.5 (config)", written)
        self.assertIn("auto-clip: ask", written)
        self.assertIn("config:", written)
        self.assertNotIn("remembered", written)
        self.assertNotIn("abc", written)

    def test_status_shows_env_model_override(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True):
                aidur.save_config({"auto_clip": "off", "last_auto_clip_md5": "", "model": "gpt-5.5"})
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path, "AIDUR_MODEL": "gpt-5.4"}, clear=True), \
                 mock.patch("sys.stdout") as stdout:
                self.assertEqual(aidur.main(["status"]), 0)
        written = "".join(call.args[0] for call in stdout.write.call_args_list if call.args)
        self.assertIn("model: gpt-5.4 (AIDUR_MODEL)", written)

    def test_models_set_saves_model(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True), \
                 mock.patch("sys.stdout") as stdout:
                self.assertEqual(aidur.main(["models", "set", "gpt-5.5"]), 0)
                self.assertEqual(aidur.load_config()["model"], "gpt-5.5")
        written = "".join(call.args[0] for call in stdout.write.call_args_list if call.args)
        self.assertIn("ai: model set to gpt-5.5", written)

    def test_models_set_rejects_unsupported_and_suggests(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True), \
                 mock.patch("urllib.request.urlopen") as urlopen, \
                 mock.patch("sys.stderr") as stderr:
                self.assertEqual(aidur.main(["models", "set", "gipt-5.5"]), 2)
        urlopen.assert_not_called()
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("unsupported Responses model", written)
        self.assertIn("Did you mean: gpt-5.5?", written)

    def test_models_set_rejects_pro_model(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True), \
                 mock.patch("sys.stderr") as stderr:
                self.assertEqual(aidur.main(["models", "set", "gpt-5.5-pro"]), 2)
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("unsupported Responses model", written)

    def test_models_list_fallback_marks_current_and_excludes_pro(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True):
                aidur.save_config({"auto_clip": "off", "last_auto_clip_md5": "", "model": "gpt-5.5"})
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True), \
                 mock.patch("sys.stdout") as stdout:
                self.assertEqual(aidur.main(["models", "list"]), 0)
        written = "".join(call.args[0] for call in stdout.write.call_args_list if call.args)
        self.assertIn("* gpt-5.5", written)
        self.assertIn("gpt-5.4-mini", written)
        self.assertNotIn("GPT 5.5", written)
        self.assertNotIn("gpt-5.5-pro", written)

    def test_models_list_filters_live_models_to_responses_gpt_non_pro(self):
        response = {
            "data": [
                {"id": "gpt-5.5", "name": "GPT 5.5", "endpoint": "https://opencode.ai/zen/v1/responses"},
                {"id": "gpt-5.5-pro", "name": "GPT 5.5 Pro", "endpoint": "https://opencode.ai/zen/v1/responses"},
                {"id": "claude-sonnet-4-6", "name": "Claude", "endpoint": "https://opencode.ai/zen/v1/messages"},
                {"id": "qwen3.6-plus", "name": "Qwen", "endpoint": "https://opencode.ai/zen/v1/chat/completions"},
            ]
        }
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path, "OPENCODE_ZEN_API_KEY": "key"}, clear=True), \
                 mock.patch("urllib.request.urlopen", return_value=FakeResponse(json.dumps(response).encode())) as urlopen, \
                 mock.patch("sys.stdout") as stdout:
                self.assertEqual(aidur.main(["models", "list"]), 0)
        urlopen.assert_called_once()
        written = "".join(call.args[0] for call in stdout.write.call_args_list if call.args)
        self.assertIn("gpt-5.5", written)
        self.assertNotIn("GPT 5.5", written)
        self.assertNotIn("gpt-5.5-pro", written)
        self.assertNotIn("claude", written)
        self.assertNotIn("qwen", written)

    def test_auto_clip_on_includes_new_clipboard_and_remembers_md5(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {**env, "AIDUR_CONFIG": config_path}, clear=True):
                aidur.save_config({"auto_clip": "on", "last_auto_clip_md5": ""})
            with mock.patch.object(aidur, "read_clipboard", return_value=("pytest failed", False, "abc")), \
                 mock.patch.dict(os.environ, {**env, "AIDUR_CONFIG": config_path}, clear=True), \
                 mock.patch("urllib.request.urlopen", return_value=FakeResponse(json.dumps({"output_text": "ok"}).encode())) as urlopen, \
                 mock.patch("sys.stdin", TtyStdin()), \
                 mock.patch("sys.stdout"), \
                 mock.patch("sys.stderr") as stderr:
                code = aidur.main(["what", "happened?"])

            self.assertEqual(code, 0)
            payload = json.loads(urlopen.call_args.args[0].data.decode())
            self.assertIn("Untrusted clipboard context", payload["input"])
            self.assertIn("pytest failed", payload["input"])
            written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
            self.assertIn("ai: sending clipboard data to agent", written)
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True):
                self.assertEqual(aidur.load_config()["last_auto_clip_md5"], "abc")

    def test_auto_clip_skips_remembered_clipboard(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True):
                aidur.save_config({"auto_clip": "on", "last_auto_clip_md5": "abc"})
            with mock.patch.object(aidur, "read_clipboard", return_value=("pytest failed", False, "abc")):
                code, urlopen, _, _ = self.run_main(
                    ["what", "happened?"],
                    {**env, "AIDUR_CONFIG": config_path},
                )

        self.assertEqual(code, 0)
        payload = json.loads(urlopen.call_args.args[0].data.decode())
        self.assertEqual(payload["input"], "what happened?")

    def test_auto_clip_ask_includes_when_user_accepts_without_sending_reminder(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True):
                aidur.save_config({"auto_clip": "ask", "last_auto_clip_md5": ""})
            with mock.patch.object(aidur, "read_clipboard", return_value=("log", False, "abc")), \
                 mock.patch.object(aidur, "prompt_include_clipboard", return_value=True):
                code, urlopen, _, stderr = self.run_main(
                    ["what?"],
                    {**env, "AIDUR_CONFIG": config_path},
                )

        self.assertEqual(code, 0)
        payload = json.loads(urlopen.call_args.args[0].data.decode())
        self.assertIn("Untrusted clipboard context", payload["input"])
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertNotIn("ai: sending clipboard data to agent", written)

    def test_auto_clip_ask_decline_remembers_md5_without_including(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True):
                aidur.save_config({"auto_clip": "ask", "last_auto_clip_md5": ""})
            with mock.patch.object(aidur, "read_clipboard", return_value=("log", False, "abc")), \
                 mock.patch.object(aidur, "prompt_include_clipboard", return_value=False):
                code, urlopen, _, _ = self.run_main(
                    ["what?"],
                    {**env, "AIDUR_CONFIG": config_path},
                )

            self.assertEqual(code, 0)
            payload = json.loads(urlopen.call_args.args[0].data.decode())
            self.assertEqual(payload["input"], "what?")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True):
                self.assertEqual(aidur.load_config()["last_auto_clip_md5"], "abc")

    def test_piped_stdin_is_included_and_skips_auto_clipboard(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with mock.patch.object(aidur, "read_clipboard") as read_clipboard:
            code, urlopen, _, _ = self.run_main(
                ["what", "happened?"],
                env,
                stdin=PipedStdin(b"command failed\n"),
            )

        self.assertEqual(code, 0)
        read_clipboard.assert_not_called()
        payload = json.loads(urlopen.call_args.args[0].data.decode())
        self.assertIn("Untrusted stdin context", payload["input"])
        self.assertIn("command failed", payload["input"])

    def test_http_error_redacts_api_key(self):
        error = HTTPError("https://x", 401, "Unauthorized", {}, io.BytesIO(b"bad secret-key"))
        with tempfile.TemporaryDirectory() as tmpdir, \
             mock.patch.dict(os.environ, {"OPENCODE_ZEN_API_KEY": "secret-key", "AIDUR_MODEL": "m", "AIDUR_CONFIG": os.path.join(tmpdir, "config.json")}, clear=True), \
             mock.patch("sys.stdin", TtyStdin()), \
             mock.patch("urllib.request.urlopen", side_effect=error), \
             mock.patch("sys.stderr") as stderr:
            self.assertEqual(aidur.main(["hello"]), 1)
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertNotIn("secret-key", written)
        self.assertIn("[REDACTED]", written)


if __name__ == "__main__":
    unittest.main()
