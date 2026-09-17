import argparse
import base64
import json
from pathlib import Path
import re
import shlex
import subprocess
from urllib.parse import urlparse

import yaml

from render import ALIPAY_KEYS, ROOT, validate_sealed_secret


def read_env(path):
    lexer = shlex.shlex(path.read_text(), posix=True)
    lexer.whitespace_split = True
    values = {}
    try:
        for token in lexer:
            if token == "export":
                continue
            key, separator, value = token.partition("=")
            if not separator or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key):
                raise ValueError("expected KEY=value")
            values[key] = value
    except ValueError:
        raise ValueError(f"invalid .env syntax near line {lexer.lineno}") from None
    return values


def alipay_secret(values, namespace):
    selected = {key: values.get(key, "").strip() for key in ALIPAY_KEYS}
    missing = sorted(key for key, value in selected.items() if not value and key != "ALIPAY_RETURN_URL")
    if missing:
        raise ValueError("missing Alipay settings: " + ", ".join(missing))
    app_id = selected["ALIPAY_APP_ID"]
    if not re.fullmatch(r"\d{16}", app_id) or app_id.startswith("2088") or app_id == selected["ALIPAY_SELLER_ID"]:
        raise ValueError("ALIPAY_APP_ID must be the sandbox application AppID, not the 2088 merchant PID")
    if not re.fullmatch(r"\d{16}", selected["ALIPAY_SELLER_ID"]):
        raise ValueError("ALIPAY_SELLER_ID must be the sandbox merchant PID")
    for key in ["ALIPAY_NOTIFY_URL", "ALIPAY_RETURN_URL"]:
        if not selected[key]:
            continue
        address = urlparse(selected[key])
        if address.scheme not in {"http", "https"} or not address.hostname or address.username or address.password:
            raise ValueError(f"{key} must be an absolute HTTP(S) URL without credentials")
    return {
        "apiVersion": "v1", "kind": "Secret", "type": "Opaque",
        "metadata": {"name": "alipay", "namespace": namespace},
        "data": {key: base64.b64encode(value.encode()).decode() for key, value in selected.items()},
    }


def main():
    parser = argparse.ArgumentParser(description="Encrypt only the Alipay .env settings for this server")
    parser.add_argument("--env", type=Path, default=ROOT / ".env")
    parser.add_argument("--cert", type=Path, required=True)
    parser.add_argument("--kubeseal", default="kubeseal")
    parser.add_argument("--output", type=Path, default=ROOT / "deploy/secrets/alipay.json")
    arguments = parser.parse_args()
    try:
        settings = yaml.safe_load((ROOT / "deploy/vps.yaml").read_text())
        secret = alipay_secret(read_env(arguments.env), settings["namespace"])
        if not arguments.cert.is_file():
            raise ValueError("sealing certificate does not exist; fetch the server's public certificate first")
        result = subprocess.run(
            [arguments.kubeseal, "--cert", str(arguments.cert), "--scope", "strict", "--format", "json"],
            input=json.dumps(secret), capture_output=True, text=True, check=False,
        )
        if result.returncode:
            raise ValueError("kubeseal failed; check the installed binary and public certificate")
        encrypted = json.loads(result.stdout)
        validate_sealed_secret(encrypted, settings["namespace"])
        arguments.output.parent.mkdir(parents=True, exist_ok=True)
        arguments.output.write_text(json.dumps(encrypted, indent=2) + "\n")
    except (OSError, ValueError) as error:
        parser.error(str(error))
    print(f"Encrypted Alipay settings written to {arguments.output}; no plaintext credentials written.")


if __name__ == "__main__":
    main()
