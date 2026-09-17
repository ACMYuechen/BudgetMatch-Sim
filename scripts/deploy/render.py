import argparse
import base64
import copy
import hashlib
import json
from pathlib import Path
import re
import sys

import yaml


ROOT = Path(__file__).resolve().parents[2]
SERVICES = {
    "auth-rpc": ("auth", "services/rpc/auth", 10003),
    "mall-rpc": ("mall", "services/rpc/mall", 10005),
    "seckill-rpc": ("seckill", "services/rpc/seckill", 10004),
    "agent-rpc": ("agent", "services/rpc/agent", 10006),
    "payment-rpc": ("payment", "services/rpc/payment", 10007),
    "app": ("app", "cmd/app", 10002),
    "admin": ("admin", "cmd/admin", 10001),
}
ALIPAY_KEYS = {
    "ALIPAY_APP_ID", "ALIPAY_SELLER_ID", "ALIPAY_PRIVATE_KEY",
    "ALIPAY_PUBLIC_KEY", "ALIPAY_NOTIFY_URL", "ALIPAY_RETURN_URL",
}
PUBLIC_ENV = {
    "ETCD_HOSTS": "etcd:2379",
    "ROCKETMQ_NAMESERVERS": "rocketmq-namesrv:9876",
    "TZ": "Asia/Shanghai",
    "GOMEMLIMIT": "180MiB",
}
SECRET_FIELDS = {
    "secret", "password", "apikey", "privatekey", "alipaypublickey",
    "accesskeysecret", "accesskeyid", "dsn",
}


def merge(target, overrides):
    for key, value in overrides.items():
        if isinstance(value, dict) and isinstance(target.get(key), dict):
            merge(target[key], value)
        else:
            target[key] = copy.deepcopy(value)


def check_secret_literals(value, location="config"):
    if isinstance(value, dict):
        for key, child in value.items():
            path = f"{location}.{key}"
            if key.lower() in SECRET_FIELDS and child:
                if not isinstance(child, str) or not re.fullmatch(r"\$\{[A-Z][A-Z0-9_]*\}", child):
                    raise ValueError(f"literal credential is not allowed at {path}")
            check_secret_literals(child, path)
    elif isinstance(value, list):
        for child in value:
            check_secret_literals(child, location)


def server_config(root, name, overrides):
    _, source_path, _ = SERVICES[name]
    config = yaml.safe_load((root / source_path / "etc/config.yaml").read_text())
    config["Mode"] = "pro"
    config["Log"] = {"Mode": "console", "Level": "info", "Encoding": "json"}
    if "Database" in config:
        config["Database"].update({
            "DSN": "${DATABASE_DSN}", "MaxOpenConns": 10,
            "MaxIdleConns": 2, "LogLevel": 2,
        })
    for cache_key in ["Redis", "CacheRedis"]:
        if cache_key in config:
            config[cache_key].update({
                "Address": "redis:6379", "Password": "${REDIS_PASSWORD}",
            })
    for rpc_name, endpoint in SERVICES.items():
        binary, _, port = endpoint
        rpc_key = f"{binary.capitalize()}Rpc"
        if rpc_key in config:
            config[rpc_key]["Endpoints"] = [f"{rpc_name}:{port}"]
    merge(config, overrides)
    check_secret_literals(config)
    return yaml.safe_dump(config, sort_keys=False, allow_unicode=True)


def validate_sealed_secret(document, namespace):
    if document.get("apiVersion") != "bitnami.com/v1alpha1" or document.get("kind") != "SealedSecret":
        raise ValueError("Alipay configuration must be an encrypted SealedSecret")
    if "data" in document or "stringData" in document:
        raise ValueError("plaintext Secret fields are not allowed")
    metadata = document.get("metadata", {})
    if metadata.get("name") != "alipay" or metadata.get("namespace") != namespace:
        raise ValueError("Alipay SealedSecret has the wrong name or namespace")
    annotations = metadata.get("annotations", {})
    if any(annotations.get(key) == "true" for key in [
        "sealedsecrets.bitnami.com/cluster-wide", "sealedsecrets.bitnami.com/namespace-wide",
    ]):
        raise ValueError("Alipay SealedSecret must use strict scope")
    spec = document.get("spec", {})
    if set(spec) - {"encryptedData", "template"}:
        raise ValueError("unexpected SealedSecret fields")
    template = spec.get("template", {})
    if set(template) - {"metadata", "type"}:
        raise ValueError("plaintext Secret templates are not allowed")
    template_metadata = template.get("metadata", {})
    if template_metadata.get("name", "alipay") != "alipay" or template_metadata.get("namespace", namespace) != namespace:
        raise ValueError("Alipay Secret template has the wrong name or namespace")
    encrypted = spec.get("encryptedData", {})
    if set(encrypted) != ALIPAY_KEYS:
        raise ValueError("Alipay SealedSecret must contain exactly the six ALIPAY settings")
    for key, value in encrypted.items():
        try:
            ciphertext = base64.b64decode(value, validate=True)
        except (ValueError, TypeError):
            raise ValueError(f"invalid ciphertext for {key}") from None
        if len(ciphertext) < 256:
            raise ValueError(f"ciphertext is too short for {key}")


def resource(kind, name, namespace, **fields):
    return {
        "apiVersion": "apps/v1" if kind == "Deployment" else "v1",
        "kind": kind,
        "metadata": {"name": name, "namespace": namespace},
        **fields,
    }


def service(name, namespace, port):
    return resource("Service", name, namespace, spec={
        "selector": {"app": name},
        "ports": [{"name": f"tcp-{port}", "port": port, "targetPort": port}],
    })


def deployment(name, namespace, image, port, request_memory, limit_memory):
    return resource("Deployment", name, namespace, spec={
        "replicas": 1,
        "strategy": {"type": "Recreate"},
        "selector": {"matchLabels": {"app": name}},
        "template": {
            "metadata": {"labels": {"app": name}, "annotations": {}},
            "spec": {
                "automountServiceAccountToken": False,
                "containers": [{
                    "name": name, "image": image, "imagePullPolicy": "IfNotPresent",
                    "ports": [{"containerPort": port}],
                    "resources": {
                        "requests": {"cpu": "25m", "memory": request_memory},
                        "limits": {"memory": limit_memory},
                    },
                    "readinessProbe": {
                        "tcpSocket": {"port": port}, "initialDelaySeconds": 3, "periodSeconds": 5,
                    },
                    "startupProbe": {
                        "tcpSocket": {"port": port}, "periodSeconds": 5, "failureThreshold": 60,
                    },
                }],
            },
        },
    })


def mount(pod, name, destination, source, read_only=False):
    pod.setdefault("volumes", []).append({"name": name, **source})
    pod["containers"][0].setdefault("volumeMounts", []).append({
        "name": name, "mountPath": destination, "readOnly": read_only,
    })


def digest(value):
    return hashlib.sha256(value.encode()).hexdigest()


def render(root, settings, backend_image, web_image, sealed_secret=None):
    for image in [backend_image, web_image]:
        if not re.fullmatch(r"[a-z0-9][a-z0-9./_-]+@sha256:[0-9a-f]{64}", image):
            raise ValueError("release images must use an immutable sha256 digest")
    namespace = settings["namespace"]
    runtime_secret = settings["runtimeSecret"]
    documents = []
    sealed_checksum = None
    if sealed_secret is not None:
        validate_sealed_secret(sealed_secret, namespace)
        sealed_secret = copy.deepcopy(sealed_secret)
        sealed_secret["metadata"].setdefault("annotations", {})["argocd.argoproj.io/sync-wave"] = "-1"
        documents.append(sealed_secret)
        sealed_checksum = digest(json.dumps(sealed_secret["spec"], sort_keys=True))
    for name, (binary, _, port) in SERVICES.items():
        options = settings.get("services", {}).get(name, {})
        config_text = server_config(root, name, options.get("config", {}))
        documents.append(resource("ConfigMap", f"{name}-config", namespace, data={"config.yaml": config_text}))
        backend = deployment(name, namespace, backend_image, port,
                             options.get("requestMemory", "48Mi"), options.get("limitMemory", "256Mi"))
        backend["metadata"]["annotations"] = {"argocd.argoproj.io/sync-wave": "1"}
        template = backend["spec"]["template"]
        template["metadata"]["annotations"]["checksum/config"] = digest(config_text)
        pod = template["spec"]
        container = pod["containers"][0]
        container.update({
            "command": [f"/app/bin/{binary}", "-f", "/app/etc/config.yaml"],
            "workingDir": "/app",
            "securityContext": {
                "allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True,
                "capabilities": {"drop": ["ALL"]},
            },
        })
        environment = []
        for key in sorted(set(re.findall(r"\$\{([A-Z][A-Z0-9_]*)\}", config_text)) - set(PUBLIC_ENV)):
            secret_name = "alipay" if name == "payment-rpc" and sealed_checksum and key in ALIPAY_KEYS else runtime_secret
            environment.append({"name": key, "valueFrom": {"secretKeyRef": {"name": secret_name, "key": key}}})
        container["env"] = environment + [{"name": key, "value": value} for key, value in PUBLIC_ENV.items()]
        pod["securityContext"] = {"runAsUser": 1000, "runAsGroup": 1000, "runAsNonRoot": True, "fsGroup": 1000}
        if settings.get("imagePullSecrets"):
            pod["imagePullSecrets"] = [{"name": name} for name in settings["imagePullSecrets"]]
        mount(pod, "config", "/app/etc", {"configMap": {"name": f"{name}-config"}}, True)
        mount(pod, "tmp", "/tmp", {"emptyDir": {}})
        if name == "agent-rpc":
            mount(pod, "workspace", "/app/workspace", {"persistentVolumeClaim": {"claimName": "agent-workspace"}})
        if name == "payment-rpc" and sealed_checksum:
            template["metadata"]["annotations"]["checksum/alipay"] = sealed_checksum
        documents.extend([service(name, namespace, port), backend])
    nginx = (root / "web-ui/nginx/default.conf").read_text().replace(
        "server_name localhost;", "server_name _;\n    client_max_body_size 50m;\n    proxy_read_timeout 600s;\n    server_tokens off;",
    )
    documents.append(resource("ConfigMap", "web-ui-config", namespace, data={"default.conf": nginx}))
    frontend = deployment("web-ui", namespace, web_image, 80, "16Mi", "64Mi")
    frontend["metadata"]["annotations"] = {"argocd.argoproj.io/sync-wave": "2"}
    frontend["spec"]["template"]["metadata"]["annotations"]["checksum/config"] = digest(nginx)
    frontend_pod = frontend["spec"]["template"]["spec"]
    if settings.get("imagePullSecrets"):
        frontend_pod["imagePullSecrets"] = [{"name": name} for name in settings["imagePullSecrets"]]
    mount(frontend_pod, "config", "/etc/nginx/conf.d", {"configMap": {"name": "web-ui-config"}}, True)
    frontend_service = service("web-ui", namespace, 80)
    frontend_service["spec"].update({
        "type": "LoadBalancer", "allocateLoadBalancerNodePorts": False,
        "ports": [{"name": "http", "port": settings["webPort"], "targetPort": 80}],
    })
    documents.extend([frontend_service, frontend])
    return documents


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--backend-image", required=True)
    parser.add_argument("--web-image", required=True)
    arguments = parser.parse_args()
    settings = yaml.safe_load((ROOT / "deploy/environments/vps.yaml").read_text())
    sealed_path = ROOT / "deploy/secrets/alipay.json"
    sealed_secret = json.loads(sealed_path.read_text()) if sealed_path.exists() else None
    try:
        documents = render(ROOT, settings, arguments.backend_image, arguments.web_image, sealed_secret)
    except ValueError as error:
        parser.error(str(error))
    yaml.safe_dump_all(documents, sys.stdout, sort_keys=False, allow_unicode=True)


if __name__ == "__main__":
    main()
