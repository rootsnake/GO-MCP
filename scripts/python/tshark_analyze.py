#!/usr/bin/env python3
"""Captura passiva e limitada com TShark, com resumo JSON sem payload.

Use apenas em interfaces e redes próprias ou para as quais tenha autorização.
A captura guarda tráfego bruto temporariamente; por omissão o ficheiro é apagado
após a análise. Use --save-pcap apenas quando for necessário conservar a captura.
"""

from __future__ import annotations

import argparse
import collections
import datetime as dt
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any

SCHEMA_VERSION = 1
DEFAULT_DURATION = 15
MAX_DURATION = 60
MAX_PACKETS = 100_000
MAX_SUMMARY_ROWS = 20


def now_utc() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat()


def emit(payload: dict[str, Any], exit_code: int = 0) -> int:
    sys.stdout.write(json.dumps(payload, ensure_ascii=False) + "\n")
    return exit_code


def error_payload(code: str, message: str) -> dict[str, Any]:
    return {
        "schema_version": SCHEMA_VERSION,
        "ok": False,
        "status": "error",
        "timestamp": now_utc(),
        "error": {"code": code, "message": message},
    }


def require_tshark() -> str:
    executable = shutil.which("tshark")
    if not executable:
        raise RuntimeError("TShark não foi encontrado no PATH do processo Python.")
    return executable


def list_interfaces(executable: str) -> list[dict[str, str]]:
    result = subprocess.run(
        [executable, "-D"], capture_output=True, text=True, timeout=15, check=False
    )
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or "TShark não conseguiu listar interfaces.")

    interfaces: list[dict[str, str]] = []
    for line in result.stdout.splitlines():
        match = re.match(r"^\s*(\d+)\.\s+(.+?)\s*$", line)
        if match:
            interfaces.append({"id": match.group(1), "name": match.group(2)})
    return interfaces


def resolve_interface(interfaces: list[dict[str, str]], requested: str) -> str:
    for interface in interfaces:
        if requested == interface["id"] or requested == interface["name"]:
            return interface["id"]
    raise ValueError("Interface não encontrada na lista do TShark; use o ID ou nome devolvido por 'interfaces'.")


def validate_capture_filter(capture_filter: str | None) -> str | None:
    if capture_filter is None:
        return None
    value = capture_filter.strip()
    if not value:
        return None
    if len(value) > 256:
        raise ValueError("O filtro de captura não pode exceder 256 caracteres.")
    if "\x00" in value or "\n" in value or "\r" in value:
        raise ValueError("O filtro contém caracteres inválidos.")
    return value


def parse_fields(text: str) -> tuple[list[str], list[dict[str, str]]]:
    protocols: collections.Counter[str] = collections.Counter()
    conversations: collections.Counter[tuple[str, str, str, str, str]] = collections.Counter()
    dns_queries: collections.Counter[str] = collections.Counter()
    packet_rows = 0

    for line in text.splitlines():
        columns = line.split("\t")
        columns += [""] * (7 - len(columns))
        proto_stack, src, dst, tcp_src, tcp_dst, udp_src, udp_dst = columns[:7]
        dns_name = columns[7] if len(columns) > 7 else ""
        packet_rows += 1

        if proto_stack:
            for protocol in proto_stack.split(":"):
                if protocol:
                    protocols[protocol] += 1

        transport = "tcp" if tcp_src or tcp_dst else "udp" if udp_src or udp_dst else "ip"
        source_port = tcp_src or udp_src
        dest_port = tcp_dst or udp_dst
        if src or dst:
            conversations[(src or "?", source_port or "", dst or "?", dest_port or "", transport)] += 1
        if dns_name:
            for name in dns_name.split(","):
                clean_name = name.strip()
                if clean_name:
                    dns_queries[clean_name] += 1

    top_conversations = [
        {"source_ip": source, "source_port": source_port or None,
         "destination_ip": destination, "destination_port": destination_port or None,
         "transport": transport, "packets": count}
        for (source, source_port, destination, destination_port, transport), count
        in conversations.most_common(MAX_SUMMARY_ROWS)
    ]
    return sorted(protocols.items(), key=lambda item: (-item[1], item[0])), {
        "packets_summarized": packet_rows,
        "top_conversations": top_conversations,
        "dns_queries": [
            {"name": name, "count": count}
            for name, count in dns_queries.most_common(MAX_SUMMARY_ROWS)
        ],
    }


def analyze_capture(executable: str, capture_path: Path) -> dict[str, Any]:
    fields = [
        "frame.protocols", "ip.src", "ip.dst", "tcp.srcport", "tcp.dstport",
        "udp.srcport", "udp.dstport", "dns.qry.name",
    ]
    command = [
        executable, "-n", "-r", str(capture_path), "-T", "fields",
        "-E", "separator=/t", "-E", "occurrence=f",
    ]
    for field in fields:
        command.extend(["-e", field])
    result = subprocess.run(command, capture_output=True, text=True, timeout=45, check=False)
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or "TShark não conseguiu analisar a captura.")
    protocols, summary = parse_fields(result.stdout)
    return {
        "packet_count": summary["packets_summarized"],
        "protocols": [{"name": name, "packet_count": count} for name, count in protocols[:MAX_SUMMARY_ROWS]],
        "top_conversations": summary["top_conversations"],
        "dns_queries": summary["dns_queries"],
    }


def capture_live(
    interface: str,
    duration: int,
    capture_filter: str | None,
    save_path: Path | None,
    max_packets: int,
) -> dict[str, Any]:
    executable = require_tshark()
    interfaces = list_interfaces(executable)
    interface_id = resolve_interface(interfaces, interface)
    capture_filter = validate_capture_filter(capture_filter)

    temporary_directory: tempfile.TemporaryDirectory[str] | None = None
    if save_path is None:
        temporary_directory = tempfile.TemporaryDirectory(prefix="tshark-capture-")
        capture_path = Path(temporary_directory.name) / "capture.pcapng"
    else:
        capture_path = save_path.expanduser().resolve()
        capture_path.parent.mkdir(parents=True, exist_ok=True)
        if capture_path.exists():
            raise ValueError("O caminho --save-pcap já existe; escolha um nome novo para não sobrescrever uma captura.")

    command = [
        executable, "-n", "-i", interface_id,
        "-a", f"duration:{duration}",
        "-a", f"packets:{max_packets}",
        "-w", str(capture_path),
    ]
    if capture_filter:
        command.extend(["-f", capture_filter])

    try:
        try:
            proc = subprocess.run(
                command,
                capture_output=True,
                text=True,
                timeout=duration + 20,
                check=False,
            )
        except subprocess.TimeoutExpired as exc:
            raise RuntimeError("A captura excedeu o limite de tempo do processo; verifique permissões e estado do TShark.") from exc

        if proc.returncode != 0:
            detail = (proc.stderr or proc.stdout).strip()[-2000:]
            raise RuntimeError(detail or "TShark não conseguiu iniciar/finalizar a captura. Verifique permissões de captura da interface.")
        if not capture_path.is_file():
            raise RuntimeError("TShark terminou sem criar o ficheiro de captura esperado.")

        summary = analyze_capture(executable, capture_path)
        return {
            "schema_version": SCHEMA_VERSION,
            "ok": True,
            "status": "completed",
            "timestamp": now_utc(),
            "capture": {
                "interface_id": interface_id,
                "interface_name": next((row["name"] for row in interfaces if row["id"] == interface_id), interface_id),
                "duration_limit_seconds": duration,
                "packet_limit": max_packets,
                "capture_filter": capture_filter,
                "saved": save_path is not None,
                "file": str(capture_path) if save_path is not None else None,
                "payload_included_in_report": False,
            },
            "summary": summary,
            "warnings": [
                "A captura pode conter dados sensíveis; partilhe o ficheiro PCAP apenas com destinatários autorizados.",
                "Tráfego cifrado normalmente expõe metadados, não o conteúdo da comunicação.",
                "A ausência de um pacote ou host na captura não prova que não exista comunicação.",
            ],
        }
    finally:
        if temporary_directory is not None:
            temporary_directory.cleanup()


def positive_int(value: str) -> int:
    try:
        number = int(value)
    except ValueError as exc:
        raise argparse.ArgumentTypeError("Tem de ser um número inteiro.") from exc
    if number < 1:
        raise argparse.ArgumentTypeError("Tem de ser maior que zero.")
    return number


def main() -> int:
    parser = argparse.ArgumentParser(description="Lista interfaces ou faz captura passiva limitada com TShark.")
    subparsers = parser.add_subparsers(dest="mode", required=True)
    subparsers.add_parser("interfaces", help="Lista interfaces disponíveis; não inicia captura.")

    capture_parser = subparsers.add_parser("capture", help="Captura tráfego ao vivo por tempo limitado e devolve metadados.")
    capture_parser.add_argument("--interface", required=True, help="ID ou nome exato da interface listado pelo modo interfaces.")
    capture_parser.add_argument("--duration", type=positive_int, default=DEFAULT_DURATION, help="Duração em segundos (predefinição: 15; máximo: 60).")
    capture_parser.add_argument("--capture-filter", help="Filtro BPF opcional, por exemplo 'host 192.168.200.1' ou 'port 53'.")
    capture_parser.add_argument("--max-packets", type=positive_int, default=MAX_PACKETS, help="Limite de pacotes (predefinição: 100000).")
    capture_parser.add_argument("--save-pcap", type=Path, help="Opcional: caminho novo onde guardar a captura bruta; sem esta opção, o ficheiro é temporário e apagado.")
    capture_parser.add_argument("--confirm-authorized", action="store_true", help="Confirma autorização para capturar nesta interface/rede.")

    args = parser.parse_args()
    try:
        executable = require_tshark()
        if args.mode == "interfaces":
            interfaces = list_interfaces(executable)
            return emit({
                "schema_version": SCHEMA_VERSION,
                "ok": True,
                "status": "listed",
                "timestamp": now_utc(),
                "interfaces": interfaces,
                "note": "A listagem não inicia captura. Capturar exige interface selecionada e autorização explícita.",
            })
        if not args.confirm_authorized:
            return emit(error_payload("authorization_required", "Captura bloqueada: confirma autorização com --confirm-authorized."), 2)
        if args.duration > MAX_DURATION:
            return emit(error_payload("duration_limit_exceeded", f"A duração máxima desta versão é {MAX_DURATION} segundos."), 2)
        if args.max_packets > MAX_PACKETS:
            return emit(error_payload("packet_limit_exceeded", f"O limite máximo desta versão é {MAX_PACKETS} pacotes."), 2)
        return emit(capture_live(args.interface, args.duration, args.capture_filter, args.save_pcap, args.max_packets))
    except (ValueError, RuntimeError, OSError, subprocess.TimeoutExpired) as exc:
        return emit(error_payload("capture_failed", str(exc)), 1)


if __name__ == "__main__":
    raise SystemExit(main())
