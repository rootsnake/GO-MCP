#!/usr/bin/env python3
"""Inventário de redes locais e descoberta controlada de hosts com Nmap.

Este script não varre redes descobertas automaticamente. O operador deve fornecer
um alvo privado explícito e confirmar que tem autorização para o verificar.
"""

from __future__ import annotations

import argparse
import datetime as dt
import ipaddress
import json
import os
import shutil
import socket
import subprocess
import sys
from typing import Any

SCHEMA_VERSION = 1
DEFAULT_SUGGESTIONS = ["192.168.200.0/24", "192.168.0.0/24", "10.0.0.0/24"]
MAX_HOSTS_DEFAULT = 256


def now_utc() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat()


def emit(payload: dict[str, Any], exit_code: int = 0) -> int:
    sys.stdout.write(json.dumps(payload, ensure_ascii=False) + "\n")
    return exit_code


def error_payload(message: str, code: str) -> dict[str, Any]:
    return {
        "schema_version": SCHEMA_VERSION,
        "ok": False,
        "status": "error",
        "timestamp": now_utc(),
        "error": {"code": code, "message": message},
    }


def discover_interfaces() -> list[dict[str, Any]]:
    """Return interface addresses visible to the OS using the standard library."""
    found: dict[str, set[str]] = {}
    try:
        hostnames = {socket.gethostname(), "localhost"}
        for hostname in hostnames:
            for item in socket.getaddrinfo(hostname, None, type=socket.SOCK_DGRAM):
                address = item[4][0].split("%", 1)[0]
                try:
                    ip = ipaddress.ip_address(address)
                except ValueError:
                    continue
                if ip.is_loopback or ip.is_unspecified:
                    continue
                found.setdefault("addresses-visible-to-python", set()).add(str(ip))
    except OSError:
        pass

    rows = []
    for name, addresses in sorted(found.items()):
        rows.append({
            "name": name,
            "type": "unknown",
            "addresses": sorted(addresses),
            "note": "Inventário básico da biblioteca padrão; não identifica com fiabilidade o tipo físico da interface nem todas as rotas.",
        })
    return rows


def discover_routes() -> tuple[list[dict[str, Any]], str | None]:
    """Read OS routing table when supported; never launch a scan."""
    commands: list[list[str]]
    if os.name == "nt":
        commands = [["route", "print", "-4"]]
    else:
        commands = [["ip", "-j", "route", "show"], ["ip", "route", "show"]]

    for command in commands:
        if shutil.which(command[0]) is None:
            continue
        try:
            proc = subprocess.run(command, capture_output=True, text=True, timeout=10, check=False)
        except (OSError, subprocess.TimeoutExpired):
            continue
        if proc.returncode != 0:
            continue
        try:
            if command[1:3] == ["-j", "route"]:
                parsed = json.loads(proc.stdout)
                routes = []
                for route in parsed:
                    dst = route.get("dst")
                    if not dst or dst == "default":
                        continue
                    try:
                        network = ipaddress.ip_network(dst, strict=False)
                    except ValueError:
                        continue
                    if network.is_private:
                        routes.append({
                            "network": str(network),
                            "interface": route.get("dev"),
                            "source": route.get("prefsrc"),
                            "discovery_source": "ip-route",
                        })
                return routes, None
            if os.name == "nt":
                routes = parse_windows_routes(proc.stdout)
                return routes, None
            routes = parse_ip_route_text(proc.stdout)
            return routes, None
        except (ValueError, TypeError):
            continue
    return [], "Não foi possível obter a tabela de rotas com os comandos disponíveis."


def parse_windows_routes(output: str) -> list[dict[str, Any]]:
    """Parse IPv4 network/mask pairs from `route print -4` output."""
    routes: list[dict[str, Any]] = []
    in_table = False
    for line in output.splitlines():
        if "Network Destination" in line and "Netmask" in line:
            in_table = True
            continue
        if not in_table:
            continue
        parts = line.split()
        if len(parts) < 4:
            continue
        try:
            network = ipaddress.ip_network(f"{parts[0]}/{parts[1]}", strict=False)
        except ValueError:
            continue
        if network.is_private and network.prefixlen > 0:
            routes.append({
                "network": str(network),
                "gateway": parts[2],
                "interface_address": parts[3],
                "discovery_source": "route-print",
            })
    # Deduplicate route entries while preserving order.
    unique: dict[tuple[str, str], dict[str, Any]] = {}
    for route in routes:
        key = (route["network"], route.get("interface_address", ""))
        unique[key] = route
    return list(unique.values())


def parse_ip_route_text(output: str) -> list[dict[str, Any]]:
    routes = []
    for line in output.splitlines():
        parts = line.split()
        if not parts or parts[0] in {"default", "unreachable", "blackhole"}:
            continue
        try:
            network = ipaddress.ip_network(parts[0], strict=False)
        except ValueError:
            continue
        if not network.is_private:
            continue
        row: dict[str, Any] = {"network": str(network), "discovery_source": "ip-route"}
        if "dev" in parts:
            row["interface"] = parts[parts.index("dev") + 1]
        if "src" in parts:
            row["source"] = parts[parts.index("src") + 1]
        routes.append(row)
    return routes


def discover() -> dict[str, Any]:
    interfaces = discover_interfaces()
    routes, warning = discover_routes()
    return {
        "schema_version": SCHEMA_VERSION,
        "ok": True,
        "status": "discovered",
        "timestamp": now_utc(),
        "interfaces": interfaces,
        "private_routes": routes,
        "suggested_targets": DEFAULT_SUGGESTIONS,
        "warnings": [warning] if warning else [],
        "note": "Redes descobertas ou sugeridas não são autorização para as analisar. Confirme o escopo com o responsável pela rede.",
    }


def parse_targets(raw_targets: list[str], max_hosts: int) -> list[dict[str, Any]]:
    resolved: list[dict[str, Any]] = []
    total = 0
    for raw in raw_targets:
        value = raw.strip()
        if not value:
            raise ValueError("Foi fornecido um alvo vazio.")
        try:
            if "-" in value and "/" not in value:
                first_text, last_text = value.split("-", 1)
                first = ipaddress.ip_address(first_text.strip())
                last = ipaddress.ip_address(last_text.strip())
                if first.version != 4 or last.version != 4 or int(last) < int(first):
                    raise ValueError(f"Intervalo IPv4 inválido: {value}")
                count = int(last) - int(first) + 1
                network_or_range = {"kind": "range", "start": str(first), "end": str(last), "address_count": count}
                addresses = [first + i for i in range(count)] if count <= max_hosts else []
                if not addresses:
                    raise ValueError(f"O intervalo {value} excede o limite de {max_hosts} endereços.")
                if not all(address.is_private for address in addresses):
                    raise ValueError(f"O intervalo {value} deve conter apenas endereços IPv4 privados.")
            elif "/" in value:
                network = ipaddress.ip_network(value, strict=False)
                if network.version != 4 or not network.is_private:
                    raise ValueError(f"O alvo CIDR deve ser uma rede IPv4 privada: {value}")
                count = network.num_addresses
                if count > max_hosts:
                    raise ValueError(f"A rede {network} contém {count} endereços e excede o limite de {max_hosts}.")
                network_or_range = {"kind": "cidr", "network": str(network), "address_count": count}
                addresses = list(network.hosts()) if network.prefixlen < 31 else list(network)
            else:
                address = ipaddress.ip_address(value)
                if address.version != 4 or not address.is_private:
                    raise ValueError(f"O alvo deve ser um endereço IPv4 privado: {value}")
                network_or_range = {"kind": "host", "address": str(address), "address_count": 1}
                addresses = [address]
        except (ipaddress.AddressValueError, ipaddress.NetmaskValueError) as exc:
            raise ValueError(f"Alvo inválido: {value}") from exc
        total += len(addresses)
        if total > max_hosts:
            raise ValueError(f"Os alvos combinados excedem o limite de {max_hosts} endereços.")
        if not all(item.version == 4 and item.is_private for item in addresses):
            raise ValueError(f"Todos os endereços devem ser IPv4 privados: {value}")
        resolved.append(network_or_range)
    if not resolved:
        raise ValueError("Informe pelo menos um alvo.")
    return resolved


def run_host_discovery(targets: list[str], max_hosts: int, timeout: int) -> dict[str, Any]:
    nmap_path = shutil.which("nmap")
    if not nmap_path:
        raise RuntimeError("Nmap não encontrado no PATH do processo Python.")

    parsed_targets = parse_targets(targets, max_hosts)
    # Pass exact validated targets as individual arguments; never invoke a shell.
    argv = [nmap_path, "-sn", "-n", "-oX", "-"] + targets
    completed = subprocess.run(
        argv,
        capture_output=True,
        text=True,
        timeout=timeout,
        check=False,
    )
    if completed.returncode not in (0, 1):
        detail = completed.stderr.strip()[-2000:]
        raise RuntimeError(f"Nmap terminou com código {completed.returncode}: {detail}")

    import xml.etree.ElementTree as ET

    try:
        root = ET.fromstring(completed.stdout)
    except ET.ParseError as exc:
        raise RuntimeError("Nmap não devolveu XML válido.") from exc

    hosts = []
    for host in root.findall("host"):
        status_node = host.find("status")
        addresses = [item.attrib for item in host.findall("address")]
        hostnames = [item.attrib.get("name") for item in host.findall("hostnames/hostname") if item.attrib.get("name")]
        ipv4 = next((item.get("addr") for item in addresses if item.get("addrtype") == "ipv4"), None)
        hosts.append({
            "ipv4": ipv4,
            "status": status_node.attrib.get("state", "unknown") if status_node is not None else "unknown",
            "reason": status_node.attrib.get("reason") if status_node is not None else None,
            "hostnames": hostnames,
            "addresses": addresses,
        })

    return {
        "schema_version": SCHEMA_VERSION,
        "ok": True,
        "status": "completed",
        "timestamp": now_utc(),
        "scan": {
            "profile": "host-discovery",
            "targets": parsed_targets,
            "nmap_arguments": ["-sn", "-n", "-oX", "-"],
            "timeout_seconds": timeout,
            "authorization_confirmed": True,
        },
        "summary": {"hosts_reported": len(hosts), "up": sum(host["status"] == "up" for host in hosts)},
        "hosts": hosts,
        "warnings": ["Host discovery pode não detetar equipamentos que bloqueiem probes ou estejam offline."],
    }


def positive_int(value: str) -> int:
    try:
        result = int(value)
    except ValueError as exc:
        raise argparse.ArgumentTypeError("Tem de ser um número inteiro.") from exc
    if result <= 0:
        raise argparse.ArgumentTypeError("Tem de ser maior que zero.")
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description="Descoberta local de redes e hosts com Nmap, em modo controlado.")
    subparsers = parser.add_subparsers(dest="mode", required=True)
    subparsers.add_parser("discover", help="Lista redes privadas/rotas conhecidas; não executa scans.")

    scan_parser = subparsers.add_parser("scan", help="Executa apenas descoberta de hosts em alvos explicitamente fornecidos.")
    scan_parser.add_argument("--target", action="append", required=True, help="IPv4, CIDR ou intervalo IPv4 START-END; pode repetir.")
    scan_parser.add_argument("--max-hosts", type=positive_int, default=MAX_HOSTS_DEFAULT, help="Limite combinado de endereços (predefinição: 256).")
    scan_parser.add_argument("--timeout", type=positive_int, default=120, help="Timeout máximo em segundos (predefinição: 120).")
    scan_parser.add_argument("--confirm-authorized", action="store_true", help="Confirma que tens autorização para verificar estes alvos.")

    args = parser.parse_args()
    if args.mode == "discover":
        return emit(discover())
    if not args.confirm_authorized:
        return emit(error_payload("Scan bloqueado: confirma autorização com --confirm-authorized.", "authorization_required"), 2)
    try:
        return emit(run_host_discovery(args.target, args.max_hosts, args.timeout))
    except (ValueError, RuntimeError, OSError, subprocess.TimeoutExpired) as exc:
        return emit(error_payload(str(exc), "scan_failed"), 1)


if __name__ == "__main__":
    raise SystemExit(main())
