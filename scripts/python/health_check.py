import argparse
import datetime
import json
import os
import platform
import sys


def main() -> int:
    parser = argparse.ArgumentParser(description="Basic host and Python runtime check")
    parser.add_argument("--tag", default="local-check", help="Optional execution tag")
    args = parser.parse_args()

    payload = {
        "ok": True,
        "tag": args.tag,
        "timestamp": datetime.datetime.now().astimezone().isoformat(),
        "hostname": platform.node(),
        "os": platform.platform(),
        "python_version": platform.python_version(),
        "cwd": os.getcwd(),
    }

    sys.stdout.write(json.dumps(payload, ensure_ascii=False) + "\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())