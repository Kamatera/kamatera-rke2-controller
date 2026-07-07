import os
import json
import subprocess

from ruamel.yaml import YAML

from kamatera_rke2_kubernetes_terraform_example_tests import setup, util, destroy


yaml = YAML(typ='safe', pure=True)


def assert_log_lines(name_prefix, datacenter_id, log_file_path=None):
    if not log_file_path:
        log_file_path = os.path.join(os.path.dirname(__file__), "kamatera-rke2-controller.log")
    expected_log_lines = [
        {"logger": "controllers.KamateraServers", "msg": "kamatera server observed", "name": f"{name_prefix}-controlplane1", "datacenter": datacenter_id, "power": "on"},
        {"logger": "controllers.KamateraServers", "msg": "kamatera server observed", "name": f"{name_prefix}-worker1", "datacenter": datacenter_id, "power": "on"},
        {"logger": "controllers.KamateraServers", "msg": "kamatera server observed", "name": f"{name_prefix}-worker3", "datacenter": datacenter_id, "power": "on"},
        {"logger": "controllers.NodeList", "msg": "node added", "node": "worker3", "ready":"True", "deleting": False, "unschedulable": False,"trackedTaints":{"DeletionCandidateOfClusterAutoscaler":False,"ToBeDeletedByClusterAutoscaler":False},"trackedAnnotations":{},"matchedServer":True,"serverName":f"{name_prefix}-worker3","serverDatacenter":datacenter_id,"serverPower":"on"},
        {"logger":"controllers.NodeList","msg":"node added","node":"controlplane1","ready":"True","deleting":False,"unschedulable":False,"trackedTaints":{"DeletionCandidateOfClusterAutoscaler":False,"ToBeDeletedByClusterAutoscaler":False},"trackedAnnotations":{},"matchedServer":True,"serverName":f"{name_prefix}-controlplane1","serverDatacenter":datacenter_id,"serverPower":"on"},
        {"logger":"controllers.NodeList","msg":"node added","node":"worker1","ready":"True","deleting":False,"unschedulable":False,"trackedTaints":{"DeletionCandidateOfClusterAutoscaler":False,"ToBeDeletedByClusterAutoscaler":False},"trackedAnnotations":{},"matchedServer":True,"serverName":f"{name_prefix}-worker1","serverDatacenter":datacenter_id,"serverPower":"on"},
        {"logger":"controllers.NodeList","msg":"node added","node":"worker2","ready":"Unknown","deleting":False,"unschedulable":False,"trackedTaints":{"DeletionCandidateOfClusterAutoscaler":False,"ToBeDeletedByClusterAutoscaler":False},"trackedAnnotations":{},"matchedServer":False,"serverName":"","serverDatacenter":"","serverPower":""},
        {"logger":"controllers.SnapshotLogger","msg":"snapshot node/server match","nodeName":"controlplane1","nodeReady":"True","serverName":f"{name_prefix}-controlplane1","serverPower":"on"},
        {"logger":"controllers.SnapshotLogger","msg":"snapshot node/server match","nodeName":"worker1","nodeReady":"True","serverName":f"{name_prefix}-worker1","serverPower":"on"},
        {"logger":"controllers.SnapshotLogger","msg":"snapshot node unmatched","nodeName":"worker2","nodeReady":"Unknown"},
        {"logger":"controllers.SnapshotLogger","msg":"snapshot node/server match","nodeName":"worker3","nodeReady":"True","serverName":f"{name_prefix}-worker3","serverPower":"on"},
        {"logger":"controllers.NodeDelete","msg":"deleted node due to NotReady timeout and Kamatera server unknown","node":"worker2","name":"worker2"},
        {"logger":"controllers.NodeList","msg":"node deleted","node":"worker2"},
        {"logger":"controllers.KamateraServers","msg":"server power changed","name":f"{name_prefix}-worker1"},
        {"logger":"controllers.KamateraServers","msg":"server power changed","name":f"{name_prefix}-worker3"},
        {"logger":"controllers.NodeDelete","msg":"deleted node due to NotReady timeout and Kamatera server powered off","node":"worker1"},
        {"logger":"controllers.NodeDelete","msg":"deleted node due to NotReady timeout and Kamatera server powered off","node":"worker3"},
    ]
    with open(log_file_path) as f:
        log_lines = []
        for line in f:
            line = line.strip()
            if line.startswith("{"):
                log_lines.append(json.loads(line))
    for expected_log_line in expected_log_lines:
        ok = False
        for actual_log_line in log_lines:
            if all(actual_log_line.get(k) == v for k, v in expected_log_line.items()):
                ok = True
                break
        assert ok, f"Expected log line not found: {expected_log_line}"


def test():
    use_existing_name_prefix = os.getenv("USE_EXISTING_NAME_PREFIX")
    name_prefix = use_existing_name_prefix or setup.generate_name_prefix()
    print(f'name_prefix="{name_prefix}"')
    k8s_version = os.getenv("K8S_VERSION") or "1.35"
    datacenter_id = "EU"
    with_bastion = False
    keep_cluster = os.getenv("KEEP_CLUSTER") == "yes"
    if use_existing_name_prefix:
        # we terminate servers so it's a problem to use existing clusters
        raise NotImplementedError("Reusing existing clusters is not implemented yet")
    setup.main(
        name_prefix=name_prefix,
        k8s_version=k8s_version,
        datacenter_id=datacenter_id,
        with_bastion=with_bastion,
        k8s_tfvars_config=None,
        extra_servers={
            "worker1": {
                "role": "rke2",
                "role_config": {
                    "rke2_type": "agent"
                },
                "cpu_cores": 2,
                "ram_mb": 4096,
            },
            "worker2": {
                "role": "rke2",
                "role_config": {
                    "rke2_type": "agent"
                },
                "cpu_cores": 2,
                "ram_mb": 4096,
            },
            "worker3": {
                "role": "rke2",
                "role_config": {
                    "rke2_type": "agent"
                },
                "cpu_cores": 2,
                "ram_mb": 4096,
            },
        },
    )
    try:
        util.wait_for(
            f"4 total and ready nodes",
            lambda: util.kubectl_node_count() == (4, 4),
            progress=lambda: util.kubectl("get", "nodes"),
        )
        util.wait_for(
            "worker2 terminated",
            lambda: destroy.cloudcli("server", "terminate", "--force", "--name", f"{name_prefix}-worker2", "--wait") or True,
        )
        util.wait_for(
            "4 total nodes, 3 ready nodes after worker2 termination",
            lambda: util.kubectl_node_count() == (4, 3),
            progress=lambda: util.kubectl("get", "nodes"),
        )
        util.kubectl(
            "apply", "-f", "rbac.yaml",
            cwd=os.path.join(os.path.dirname(__file__), '..', '..', 'deploy')
        )
        token = util.kubectl(
            "create", "token", "kamatera-rke2-controller", "-n", "kube-system", "--duration", "24h", parse_json=True
        )["status"]["token"]
        with open(util.get_kubeconfig()) as f:
            kubeconfig = yaml.load(f)
        kubeconfig["users"] = [
            {
                "name": "cluster-autoscaler",
                "user": {
                    "token": token
                }
            }
        ]
        kubeconfig["contexts"][0]["context"]["user"] = "cluster-autoscaler"
        rc_kubeconfig = os.path.join(os.path.dirname(__file__), ".kubeconfig")
        with open(rc_kubeconfig, "w") as f:
            yaml.dump(kubeconfig, f)
        rc_args = [
            "../../bin/kamatera-rke2-controller",
            "-kubeconfig", rc_kubeconfig,
            "-match-node-to-server-template", f"{name_prefix}-%s",
            "-kamatera-server-name-glob", f"{name_prefix}-*",
            "-kamatera-server-datacenters", datacenter_id,
            "-not-ready-duration", "2m",
            "-node-delete-poll-interval", "5s",
            "-kamatera-server-list-interval", "5s",
            "-snapshots-log-interval", "1m",
        ]
        print("Starting controller:", " ".join(rc_args))
        with open(os.path.join(os.path.dirname(__file__), "kamatera-rke2-controller.log"), "wb") as f:
            rc_p = subprocess.Popen(rc_args, cwd=os.path.dirname(__file__), stderr=subprocess.STDOUT, stdout=f)
            try:
                util.wait_for(
                    "3 total nodes, 3 ready nodes after controller deleted unready node",
                    lambda: util.kubectl_node_count() == (3, 3),
                    progress=lambda: util.kubectl("get", "nodes"),
                )
                util.wait_for(
                    "worker 1 power off",
                    lambda: destroy.cloudcli("server", "poweroff", "--name", f"{name_prefix}-worker1", "--wait") or True,
                )
                util.wait_for(
                    "worker 3 power off",
                    lambda: destroy.cloudcli("server", "poweroff", "--name", f"{name_prefix}-worker3", "--wait") or True,
                )
                util.wait_for(
                    "1 ready nodes after controller deleted unready nodes",
                    lambda: util.kubectl_node_count() == (1, 1),
                    progress=lambda: util.kubectl("get", "nodes"),
                )
            finally:
                if rc_p:
                    rc_p.terminate()
                    rc_p.wait()
        try:
            assert_log_lines(name_prefix, datacenter_id)
        finally:
            with open(os.path.join(os.path.dirname(__file__), "kamatera-rke2-controller.log")) as f:
                print(f.read())
    except:
        try:
            util.kubectl("get", "nodes")
        except:
            pass
        print(f'name_prefix="{name_prefix}"')
        raise
    else:
        if keep_cluster:
            print(f'name_prefix="{name_prefix}"')
        else:
            util.wait_for(
                "cluster to be destroyed",
                lambda: destroy.main(name_prefix=name_prefix, datacenter_id=datacenter_id) or True,
                retry_on_exception=True
            )
