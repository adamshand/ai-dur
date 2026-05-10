import importlib.util
import importlib.machinery
import io
import json
import os
import pathlib
import tempfile
import unittest
from unittest import mock
from types import SimpleNamespace
from urllib.error import HTTPError

SCRIPT = pathlib.Path(__file__).resolve().parents[1] / "dur"
loader = importlib.machinery.SourceFileLoader("aidur", str(SCRIPT))
spec = importlib.util.spec_from_loader("aidur", loader)
aidur = importlib.util.module_from_spec(spec)
loader.exec_module(aidur)


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
        self.assertTrue(payload["stream"])
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
        self.assertEqual(request.get_header("User-agent"), "dur/0.1")

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
        self.assertIn("Usage: dur [--debug] [--include-clipboard] <question>", written)
        self.assertIn("dur config include-clipboard always|ask|never", written)
        self.assertIn("dur config streaming on|off", written)
        self.assertIn("dur config model <model>", written)
        self.assertIn("dur models", written)
        self.assertIn("dur status", written)

    def test_help_commands_print_usage_without_api_call(self):
        for argv in (["help"], ["--help"], ["-h"]):
            with self.subTest(argv=argv), \
                 mock.patch("urllib.request.urlopen") as urlopen, \
                 mock.patch("sys.stderr") as stderr:
                self.assertEqual(aidur.main(list(argv)), 2)
            urlopen.assert_not_called()
            written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
            self.assertIn("Usage: dur [--debug] [--include-clipboard] <question>", written)

    def test_clip_uses_osc52_context(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with mock.patch.object(aidur, "read_clipboard", return_value=("pytest failed\n", False, "md5")) as read_clipboard:
            code, urlopen, _, _ = self.run_main(
                ["--include-clipboard", "why", "did", "this", "fail?"],
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
            code, urlopen, _, _ = self.run_main(["--include-clipboard"], env)

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
            code = aidur.main(["--include-clipboard", "what", "is", "this?"])

        self.assertEqual(code, 2)
        urlopen.assert_not_called()
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("dur: clipboard is empty", written)

    def test_clip_reports_osc52_unavailable(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with tempfile.TemporaryDirectory() as tmpdir, \
             mock.patch.dict(os.environ, {**env, "AIDUR_CONFIG": os.path.join(tmpdir, "config.json")}, clear=True), \
             mock.patch.object(aidur, "read_clipboard", side_effect=aidur.ClipboardUnavailable("OSC 52 query timed out")), \
             mock.patch("sys.stdin", TtyStdin()), \
             mock.patch("urllib.request.urlopen") as urlopen, \
             mock.patch("sys.stderr") as stderr:
            code = aidur.main(["--include-clipboard", "why?"])

        self.assertEqual(code, 1)
        urlopen.assert_not_called()
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("dur: clipboard unavailable: OSC 52 query timed out", written)

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

    def test_clipboard_timeout_is_configurable(self):
        with mock.patch.dict(os.environ, {"AIDUR_CLIPBOARD_TIMEOUT_SECONDS": "7.5"}, clear=True):
            self.assertEqual(aidur.get_clipboard_timeout(), 7.5)

    def test_clipboard_prompt_waits_for_user_input(self):
        fake_fd = 42
        with mock.patch.object(aidur.os, "open", return_value=fake_fd), \
             mock.patch.object(aidur.os, "write"), \
             mock.patch.object(aidur.os, "read", return_value=b"\n"), \
             mock.patch.object(aidur.os, "close"), \
             mock.patch.object(aidur.termios, "tcgetattr", return_value=[0]), \
             mock.patch.object(aidur.termios, "tcsetattr"), \
             mock.patch.object(aidur.tty, "setcbreak") as setcbreak, \
             mock.patch.object(aidur.select, "select", return_value=([fake_fd], [], [])) as select_call:
            self.assertTrue(aidur.prompt_include_clipboard())

        setcbreak.assert_called_once_with(fake_fd)
        self.assertIsNone(select_call.call_args.args[3])

    def test_streaming_commands_update_config(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            env = {"AIDUR_CONFIG": config_path}
            with mock.patch.dict(os.environ, env, clear=True), mock.patch("sys.stdout"):
                self.assertEqual(aidur.main(["config", "streaming", "off"]), 0)
                self.assertEqual(aidur.load_config()["streaming"], "off")
                self.assertEqual(aidur.main(["config", "streaming", "on"]), 0)
                self.assertEqual(aidur.load_config()["streaming"], "on")

    def test_streaming_off_omits_stream_flag(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True):
                aidur.save_config({"auto_clip": "off", "last_auto_clip_md5": "", "model": "", "streaming": "off"})
            code, urlopen, _, _ = self.run_main(["hello"], {**env, "AIDUR_CONFIG": config_path})
        self.assertEqual(code, 0)
        payload = json.loads(urlopen.call_args.args[0].data.decode())
        self.assertNotIn("stream", payload)

    def test_streaming_sse_prints_deltas(self):
        body = (
            b"event: response.output_text.delta\n"
            b"data: {\"delta\":\"hel\"}\n\n"
            b"event: response.output_text.delta\n"
            b"data: {\"delta\":\"lo\"}\n\n"
            b"event: response.completed\n"
            b"data: {}\n\n"
        )
        code, _, stdout, _ = self.run_main(
            ["hello"],
            {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"},
            response_body=body,
        )
        self.assertEqual(code, 0)
        written = "".join(call.args[0] for call in stdout.write.call_args_list if call.args)
        self.assertEqual(written, "hello\n")

    def test_streaming_command_typo_does_not_call_api(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True), \
                 mock.patch("urllib.request.urlopen") as urlopen, \
                 mock.patch("sys.stderr") as stderr:
                self.assertEqual(aidur.main(["config", "streaming", "wat"]), 2)
        urlopen.assert_not_called()
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("dur config streaming on|off", written)

    def test_auto_clip_commands_update_config(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            env = {"AIDUR_CONFIG": config_path}
            with mock.patch.dict(os.environ, env, clear=True), mock.patch("sys.stdout"):
                self.assertEqual(aidur.main(["config", "include-clipboard", "ask"]), 0)
                self.assertEqual(aidur.load_config()["auto_clip"], "ask")
                self.assertEqual(aidur.main(["config", "include-clipboard", "never"]), 0)
                self.assertEqual(aidur.load_config()["auto_clip"], "off")
                self.assertEqual(aidur.main(["config", "include-clipboard", "always"]), 0)
                self.assertEqual(aidur.load_config()["auto_clip"], "on")

    def test_auto_clip_command_typo_does_not_call_api(self):
        for argv in (["config", "include-clipboard", "wat"], ["config", "include-clipboard"], ["config", "wat"]):
            with self.subTest(argv=argv), tempfile.TemporaryDirectory() as tmpdir:
                config_path = os.path.join(tmpdir, "config.json")
                with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True), \
                     mock.patch("urllib.request.urlopen") as urlopen, \
                     mock.patch("sys.stderr") as stderr:
                    self.assertEqual(aidur.main(list(argv)), 2)
            urlopen.assert_not_called()
            written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
            self.assertIn("Usage: dur config include-clipboard always|ask|never", written)

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
        self.assertIn("include-clipboard: ask", written)
        self.assertIn("streaming: on", written)
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
                self.assertEqual(aidur.main(["config", "model", "gpt-5.5"]), 0)
                self.assertEqual(aidur.load_config()["model"], "gpt-5.5")
        written = "".join(call.args[0] for call in stdout.write.call_args_list if call.args)
        self.assertIn("dur: model set to gpt-5.5", written)

    def test_models_set_rejects_unsupported_and_suggests(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True), \
                 mock.patch("urllib.request.urlopen") as urlopen, \
                 mock.patch("sys.stderr") as stderr:
                self.assertEqual(aidur.main(["config", "model", "gipt-5.5"]), 2)
        urlopen.assert_not_called()
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("unsupported Responses model", written)
        self.assertIn("Did you mean: gpt-5.5?", written)

    def test_models_set_rejects_pro_model(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True), \
                 mock.patch("sys.stderr") as stderr:
                self.assertEqual(aidur.main(["config", "model", "gpt-5.5-pro"]), 2)
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("unsupported Responses model", written)

    def test_models_list_fallback_marks_current_and_excludes_pro(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True):
                aidur.save_config({"auto_clip": "off", "last_auto_clip_md5": "", "model": "gpt-5.5"})
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True), \
                 mock.patch("sys.stdout") as stdout:
                self.assertEqual(aidur.main(["models"]), 0)
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
                self.assertEqual(aidur.main(["models"]), 0)
        urlopen.assert_called_once()
        written = "".join(call.args[0] for call in stdout.write.call_args_list if call.args)
        self.assertIn("gpt-5.5", written)
        self.assertNotIn("GPT 5.5", written)
        self.assertNotIn("gpt-5.5-pro", written)
        self.assertNotIn("claude", written)
        self.assertNotIn("qwen", written)

    def test_auto_clip_off_mentions_clipboard_adds_status(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        code, urlopen, _, _ = self.run_main(
            ["can", "you", "see", "the", "clipboard?"],
            env,
        )
        self.assertEqual(code, 0)
        payload = json.loads(urlopen.call_args.args[0].data.decode())
        self.assertIn("Clipboard status", payload["input"])
        self.assertIn("persistent clipboard inclusion is disabled", payload["input"])

    def test_auto_clip_empty_clipboard_adds_status(self):
        env = {"OPENCODE_ZEN_API_KEY": "key", "AIDUR_MODEL": "model"}
        with tempfile.TemporaryDirectory() as tmpdir:
            config_path = os.path.join(tmpdir, "config.json")
            with mock.patch.dict(os.environ, {"AIDUR_CONFIG": config_path}, clear=True):
                aidur.save_config({"auto_clip": "on", "last_auto_clip_md5": "", "model": "", "streaming": "on"})
            with mock.patch.object(aidur, "read_clipboard", return_value=("", False, "abc")):
                code, urlopen, _, _ = self.run_main(
                    ["can", "you", "see", "the", "clipboard?"],
                    {**env, "AIDUR_CONFIG": config_path},
                )
        self.assertEqual(code, 0)
        payload = json.loads(urlopen.call_args.args[0].data.decode())
        self.assertIn("Clipboard status", payload["input"])
        self.assertIn("clipboard was empty", payload["input"])

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
            self.assertIn("dur: sending clipboard data to agent", written)
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
        self.assertIn("Clipboard status", payload["input"])
        self.assertIn("already handled", payload["input"])

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
        self.assertNotIn("dur: sending clipboard data to agent", written)

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
            self.assertIn("Clipboard status", payload["input"])
            self.assertIn("user declined", payload["input"])
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

    def test_debug_prints_request_payload_without_api_key(self):
        env = {"OPENCODE_ZEN_API_KEY": "secret-key", "AIDUR_MODEL": "model"}
        code, _, _, stderr = self.run_main(["--debug", "hello"], env)
        self.assertEqual(code, 0)
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("--- dur request ---", written)
        self.assertIn("\"model\": \"model\"", written)
        self.assertIn("\"input\": \"hello\"", written)
        self.assertIn("\"instructions\"", written)
        self.assertNotIn("secret-key", written)

    def test_debug_and_include_clipboard_flags_are_order_insensitive(self):
        env = {"OPENCODE_ZEN_API_KEY": "secret-key", "AIDUR_MODEL": "model"}
        with mock.patch.object(aidur, "read_clipboard", return_value=("clip text", False, "md5")):
            code, urlopen, _, stderr = self.run_main(["--include-clipboard", "--debug", "what?"], env)
        self.assertEqual(code, 0)
        payload = json.loads(urlopen.call_args.args[0].data.decode())
        self.assertIn("clip text", payload["input"])
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("--- dur request ---", written)
        self.assertNotIn("secret-key", written)

    def test_debug_help_prints_usage_without_api_call(self):
        with mock.patch("urllib.request.urlopen") as urlopen, \
             mock.patch("sys.stderr") as stderr:
            self.assertEqual(aidur.main(["--debug", "help"]), 2)
        urlopen.assert_not_called()
        written = "".join(call.args[0] for call in stderr.write.call_args_list if call.args)
        self.assertIn("Usage: dur", written)

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

    def test_chat_command_enters_ephemeral_chat(self):
        with mock.patch.object(aidur, "run_chat", return_value=0) as run_chat:
            self.assertEqual(aidur.main(["chat"]), 0)
        run_chat.assert_called_once_with(debug=False)

    def test_readonly_tool_rejects_shell_redirection(self):
        result = aidur.run_readonly_command("cat", [">", "out.txt"], os.getcwd())
        self.assertIn("denied", result)
        self.assertIn("shell syntax", result)

    def test_docker_logs_injects_tail_and_denies_follow(self):
        completed = SimpleNamespace(returncode=0, stdout=b"ok", stderr=b"")
        with mock.patch.object(aidur, "resolve_tool_binary", return_value="/usr/bin/docker"), \
             mock.patch.object(aidur.subprocess, "run", return_value=completed) as run:
            result = aidur.run_readonly_command("docker", ["logs", "web"], os.getcwd())
        self.assertIn("exit_code: 0", result)
        self.assertEqual(run.call_args.args[0], ["/usr/bin/docker", "logs", "--tail", "200", "web"])
        denied = aidur.run_readonly_command("docker", ["logs", "-f", "web"], os.getcwd())
        self.assertIn("follow mode is not allowed", denied)
        unbounded = aidur.run_readonly_command("docker", ["logs", "--tail", "all", "web"], os.getcwd())
        self.assertIn("docker logs tail must be a number", unbounded)

    def test_ping_injects_bounds_and_denies_flood(self):
        completed = SimpleNamespace(returncode=0, stdout=b"ok", stderr=b"")
        with mock.patch.object(aidur, "resolve_tool_binary", return_value="/bin/ping"), \
             mock.patch.object(aidur.subprocess, "run", return_value=completed) as run:
            aidur.run_readonly_command("ping", ["example.com"], os.getcwd())
        argv = run.call_args.args[0]
        self.assertIn("-c", argv)
        self.assertIn("4", argv)
        self.assertIn("-w", argv)
        self.assertIn("8", argv)
        denied = aidur.run_readonly_command("ping", ["-f", "example.com"], os.getcwd())
        self.assertIn("flood mode is not allowed", denied)
        too_many = aidur.run_readonly_command("ping", ["-c", "999", "example.com"], os.getcwd())
        self.assertIn("ping count must be no greater than 10", too_many)

    def test_safe_find_denies_mutating_actions_and_finds_files(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            pathlib.Path(tmpdir, "app.py").write_text("print('hi')\n")
            result = aidur.run_readonly_command("find", [".", "-name", "*.py", "-type", "f"], tmpdir)
            denied = aidur.run_readonly_command("find", [".", "-delete"], tmpdir)
        self.assertIn("app.py", result)
        self.assertIn("find action not allowed", denied)

    def test_readonly_tool_rejects_mutating_ip_dmesg_and_hostname(self):
        self.assertIn("mutating ip", aidur.run_readonly_command("ip", ["link", "set", "eth0", "down"], os.getcwd()))
        self.assertIn("dmesg option not allowed", aidur.run_readonly_command("dmesg", ["--clear"], os.getcwd()))
        self.assertIn("hostname arguments are not allowed", aidur.run_readonly_command("hostname", ["new-host"], os.getcwd()))
        self.assertIn("journalctl line count", aidur.run_readonly_command("journalctl", ["-n", "all"], os.getcwd()))


if __name__ == "__main__":
    unittest.main()
