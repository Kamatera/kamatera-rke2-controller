import os
import subprocess

from ruamel.yaml import YAML

from kamatera_rke2_kubernetes_terraform_example_tests import setup, util, destroy


yaml = YAML(typ='safe', pure=True)


def test():
    use_existing_name_prefix = os.getenv("USE_EXISTING_NAME_PREFIX")
    name_prefix = use_existing_name_prefix or setup.generate_name_prefix()
    print(f'name_prefix="{name_prefix}"')
    k8s_version = os.getenv("K8S_VERSION") or "1.35"
    datacenter_id = "US-NY2"
    with_bastion = False
    keep_cluster = os.getenv("KEEP_CLUSTER") == "yes"
    rc_p = None
    try:
        if use_existing_name_prefix:
            raise NotImplementedError("Reusing existing clusters is not implemented yet")
        else:
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
        util.wait_for(
            f"4 total and ready nodes",
            lambda: util.kubectl_node_count() == (4, 4),
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
            "-not-ready-duration", "2m",
        ]
        print("Starting controller:", " ".join(rc_args))
        rc_p = subprocess.Popen(rc_args, cwd=os.path.dirname(__file__), stderr=subprocess.STDOUT, stdout=subprocess.PIPE)
        util.wait_for(
            "worker2 terminated",
            lambda: destroy.cloudcli("server", "terminate", "--force", "--name", f"{name_prefix}-worker2", "--wait") or True,
        )
        util.wait_for(
            "4 total nodes, 3 ready nodes after worker2 termination",
            lambda: util.kubectl_node_count() == (3, 2),
            progress=lambda: util.kubectl("get", "nodes"),
        )
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
            "worker 2 power off",
            lambda: destroy.cloudcli("server", "poweroff", "--name", f"{name_prefix}-worker2", "--wait") or True,
        )
        util.wait_for(
            "1 ready nodes after controller deleted unready nodes",
            lambda: util.kubectl_node_count() == (1, 1),
            progress=lambda: util.kubectl("get", "nodes"),
        )
    except:
        if rc_p:
            rc_p.terminate()
            rc_p.wait()
            for line in rc_p.stdout:
                print(line.decode().rstrip())
        util.kubectl("get", "nodes")
        print(f'name_prefix="{name_prefix}"')
        raise
    else:
        if rc_p:
            rc_p.terminate()
            rc_p.wait()
        if keep_cluster:
            print(f'name_prefix="{name_prefix}"')
        else:
            destroy.main(
                name_prefix=name_prefix,
                datacenter_id=datacenter_id,
            )
