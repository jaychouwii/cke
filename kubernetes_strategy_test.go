package cke

import "testing"

type KubernetesTestConfiguration struct {
	// Cluster
	CpNodes    []string
	NonCpNodes []string

	// Running in ClusterStatus
	Rivers             []string
	APIServers         []string
	ControllerManagers []string
	Schedulers         []string
}

type CommandInfo struct {
	Name   string
	Target string
}

func (c *KubernetesTestConfiguration) Cluster() *Cluster {
	var nodes = make([]*Node, len(c.CpNodes)+len(c.NonCpNodes))
	for i, n := range c.CpNodes {
		nodes[i] = &Node{Address: n, ControlPlane: true}
	}
	for i, n := range c.NonCpNodes {
		nodes[i+len(c.CpNodes)] = &Node{Address: n}
	}
	return &Cluster{Nodes: nodes}
}

func (c *KubernetesTestConfiguration) ClusterState() *ClusterStatus {
	nodeStatus := make(map[string]*NodeStatus)
	for _, addr := range append(c.CpNodes, c.NonCpNodes...) {
		nodeStatus[addr] = &NodeStatus{}
	}
	for _, addr := range c.Rivers {
		nodeStatus[addr].Rivers.Running = true
	}
	for _, addr := range c.APIServers {
		nodeStatus[addr].APIServer.Running = true
	}
	for _, addr := range c.ControllerManagers {
		nodeStatus[addr].ControllerManager.Running = true
	}
	for _, addr := range c.Schedulers {
		nodeStatus[addr].Scheduler.Running = true
	}

	return &ClusterStatus{NodeStatuses: nodeStatus}
}

func testKubernetesDecideToDo(t *testing.T) {
	cpNodes := []string{"10.0.0.11", "10.0.0.12", "10.0.0.13"}
	nonCpNodes := []string{"10.0.0.14", "10.0.0.15", "10.0.0.16"}

	cases := []struct {
		Name     string
		Input    KubernetesTestConfiguration
		Commands []CommandInfo
	}{
		{
			Name: "Bootstrap Rivers",
			Input: KubernetesTestConfiguration{
				CpNodes: cpNodes, NonCpNodes: nonCpNodes,
			},
			Commands: []CommandInfo{
				{"image-pull", "rivers"},
				{"mkdir", "/var/log/rivers"},
				{"run-container", "10.0.0.11"},
				{"run-container", "10.0.0.12"},
				{"run-container", "10.0.0.13"},
			},
		},
		{
			Name: "Bootstrap APIServers",
			Input: KubernetesTestConfiguration{
				CpNodes: cpNodes, NonCpNodes: nonCpNodes,
				Rivers: cpNodes,
			},
			Commands: []CommandInfo{
				{"image-pull", "kube-apiserver"},
				{"mkdir", "/var/log/kubernetes/apiserver"},
				{"run-container", "10.0.0.11"},
				{"run-container", "10.0.0.12"},
				{"run-container", "10.0.0.13"},
			},
		},
		{
			Name: "Bootstrap ControllerManagers",
			Input: KubernetesTestConfiguration{
				CpNodes: cpNodes, NonCpNodes: nonCpNodes,
				Rivers: cpNodes, APIServers: cpNodes,
			},
			Commands: []CommandInfo{
				{"make-file", "/etc/kubernetes/controller-manager/kubeconfig"},
				{"image-pull", "kube-controller-manager"},
				{"mkdir", "/var/log/kubernetes/controller-manager"},
				{"run-container", "10.0.0.11"},
				{"run-container", "10.0.0.12"},
				{"run-container", "10.0.0.13"},
			},
		},
		{
			Name: "Bootstrap Scheduler",
			Input: KubernetesTestConfiguration{
				CpNodes: cpNodes, NonCpNodes: nonCpNodes,
				Rivers: cpNodes, APIServers: cpNodes, ControllerManagers: cpNodes,
			},
			Commands: []CommandInfo{
				{"make-file", "/etc/kubernetes/scheduler/kubeconfig"},
				{"image-pull", "kube-scheduler"},
				{"mkdir", "/var/log/kubernetes/scheduler"},
				{"run-container", "10.0.0.11"},
				{"run-container", "10.0.0.12"},
				{"run-container", "10.0.0.13"},
			},
		},
		{
			Name: "Stop Rivers",
			Input: KubernetesTestConfiguration{
				CpNodes: cpNodes, NonCpNodes: nonCpNodes,
				Rivers: append(cpNodes, "10.0.0.14", "10.0.0.15"),
			},
			Commands: []CommandInfo{
				{"stop-container", "10.0.0.14"},
				{"stop-container", "10.0.0.15"},
			},
		},
		{
			Name: "Stop APIServers",
			Input: KubernetesTestConfiguration{
				CpNodes: cpNodes, NonCpNodes: nonCpNodes,
				Rivers: cpNodes, APIServers: append(cpNodes, "10.0.0.14", "10.0.0.15"),
			},
			Commands: []CommandInfo{
				{"stop-container", "10.0.0.14"},
				{"stop-container", "10.0.0.15"},
			},
		},
		{
			Name: "Stop Controller Managers",
			Input: KubernetesTestConfiguration{
				CpNodes: cpNodes, NonCpNodes: nonCpNodes,
				Rivers: cpNodes, APIServers: cpNodes,
				ControllerManagers: append(cpNodes, "10.0.0.14", "10.0.0.15"),
			},
			Commands: []CommandInfo{
				{"rm", "/etc/kubernetes/controller-manager/kubeconfig"},
				{"stop-container", "10.0.0.14"},
				{"stop-container", "10.0.0.15"},
			},
		},
		{
			Name: "Stop Schedulers",
			Input: KubernetesTestConfiguration{
				CpNodes: cpNodes, NonCpNodes: nonCpNodes,
				Rivers: cpNodes, APIServers: cpNodes, ControllerManagers: cpNodes,
				Schedulers: append(cpNodes, "10.0.0.14", "10.0.0.15"),
			},
			Commands: []CommandInfo{
				{"rm", "/etc/kubernetes/scheduler/kubeconfig"},
				{"stop-container", "10.0.0.14"},
				{"stop-container", "10.0.0.15"},
			},
		},
	}

	for _, c := range cases {
		op := kubernetesDecideToDo(c.Input.Cluster(), c.Input.ClusterState())
		if op == nil {
			t.Fatal("op == nil")
		}
		cmds := opCommands(op)
		if len(c.Commands) != len(cmds) {
			t.Errorf("[%s] commands length mismatch: %d", c.Name, len(cmds))
			continue
		}
		for i, res := range cmds {
			cmd := c.Commands[i]
			if cmd.Name != res.Name {
				t.Errorf("[%s] command name mismatch: %s != %s", c.Name, cmd.Name, res.Name)
			}
			if cmd.Target != res.Target {
				t.Errorf("[%s] command '%s' target mismatch: %s != %s", c.Name, cmd.Name, cmd.Target, res.Target)
			}
		}
	}
}

func TestKubernetesStrategy(t *testing.T) {
	t.Run("KubernetesDecideToDo", testKubernetesDecideToDo)
}
