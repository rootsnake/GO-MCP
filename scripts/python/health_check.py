import argparse
import datetime as dt
import json
import os
import platform
import shutil
import sys
import tempfile
from pathlib import Path
from typing import Any


def check_working_directory() -> dict[str, Any]:
    cwd = Path.cwd()
    passed = cwd.is_dir() and os.access(cwd, os.R_OK | os.X_OK)
    return {
        "name": "working_directory",
        "status": "ok" if passed else "error",
        "message": "Diretório atual acessível." if passed else "Diretório atual não existe ou não pode ser lido.",
        "details": {"path": str(cwd)},
    }


def check_temp_writable() -> dict[str, Any]:
    try:
        with tempfile.NamedTemporaryFile(prefix="mcp-health-check-", delete=True):
            pass
        return {
            "name": "temp_directory_writable",
            "status": "ok",
            "message": "Diretório temporário permite criar e remover ficheiros.",
        }
    except OSError as exc:
        return {
            "name": "temp_directory_writable",
            "status": "error",
            "message": "Não foi possível criar um ficheiro temporário.",
            "details": {"error": str(exc)},
        }


def check_disk_space(path: Path, min_free_mb: int) -> dict[str, Any]:
    try:
        usage = shutil.disk_usage(path)
        free_mb = round(usage.free / (1024 * 1024), 2)
        passed = free_mb >= min_free_mb
        return {
            "name": "disk_space",
            "status": "ok" if passed else "warning",
            "message": (
                "Espaço livre acima do limite configurado."
                if passed
                else "Espaço livre abaixo do limite configurado."
            ),
            "details": {
                "path": str(path),
                "total_mb": round(usage.total / (1024 * 1024), 2),
                "free_mb": free_mb,
                "minimum_free_mb": min_free_mb,
            },
        }
    except OSError as exc:
        return {
            "name": "disk_space",
            "status": "error",
            "message": "Não foi possível consultar o espaço em disco.",
            "details": {"path": str(path), "error": str(exc)},
        }


def build_report(tag: str, min_free_mb: int) -> dict[str, Any]:
    cwd = Path.cwd()
    checks = [
        check_working_directory(),
        check_temp_writable(),
        check_disk_space(cwd, min_free_mb),
    ]

    has_error = any(check["status"] == "error" for check in checks)
    has_warning = any(check["status"] == "warning" for check in checks)
    overall_status = "unhealthy" if has_error else "degraded" if has_warning else "healthy"

    return {
        "schema_version": 1,
        "ok": not has_error,
        "status": overall_status,
        "tag": tag,
        "timestamp": dt.datetime.now(dt.timezone.utc).isoformat(),
        "environment": {
            "hostname": platform.node(),
            "os": platform.platform(),
            "python_version": platform.python_version(),
            "cwd": str(cwd),
        },
        "checks": checks,
        "summary": {
            "total": len(checks),
            "passed": sum(check["status"] == "ok" for check in checks),
            "warnings": sum(check["status"] == "warning" for check in checks),
            "errors": sum(check["status"] == "error" for check in checks),
        },
    }


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Verifica condições básicas do ambiente onde o processo Python está a correr."
    )
    parser.add_argument("--tag", default="local-check", help="Etiqueta opcional da execução")
    parser.add_argument(
        "--min-free-mb",
        type=int,
        default=512,
        help="Limite mínimo de espaço livre em MB; abaixo disso gera aviso (predefinição: 512)",
    )
    args = parser.parse_args()

    if args.min_free_mb < 0:
        parser.error("--min-free-mb não pode ser negativo")

    report = build_report(args.tag, args.min_free_mb)
    sys.stdout.write(json.dumps(report, ensure_ascii=False) + "\n")
    return 1 if not report["ok"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
