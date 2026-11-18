// report_test.go：报告统计与输出的单元测试。
package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-inspector/internal/collect"
)

// 与 collect 包测试相同的极简构造辅助。跨包不能复用未导出的测试函数，
// 这几个辅助函数只有几行，就地复制一份是最简单的做法。
func testNode(name string, conditions ...corev1.NodeCondition) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status:     corev1.NodeStatus{Conditions: conditions},
	}
}

func runningState() corev1.ContainerState {
	return corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
}

func waitingState(reason string) corev1.ContainerState {
	return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}
}

func mainContainer(name string, ready bool, restarts int32, state corev1.ContainerState) corev1.ContainerStatus {
	return corev1.ContainerStatus{Name: name, Ready: ready, RestartCount: restarts, State: state}
}

func testPod(namespace, name string, phase corev1.PodPhase, containers []corev1.ContainerStatus) corev1.Pod {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Status:     corev1.PodStatus{Phase: phase, PodIP: "10.42.0.15", ContainerStatuses: containers},
	}
	for _, cs := range containers {
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: cs.Name})
	}
	return pod
}

func TestSummarizeNodes(t *testing.T) {
	nodes := []corev1.Node{
		testNode("node-a", corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue}),
		testNode("node-b", corev1.NodeCondition{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionFalse,
			Reason: "KubeletNotReady",
		}),
	}

	var infos []collect.NodeInspection
	for i := range nodes {
		infos = append(infos, collect.InspectNode(&nodes[i]))
	}
	stats := SummarizeNodes(infos)

	if stats.Total != 2 || stats.Ready != 1 || stats.NotReady != 1 {
		t.Fatalf("统计不正确: Total=%d Ready=%d NotReady=%d", stats.Total, stats.Ready, stats.NotReady)
	}
	if len(stats.Normal) != 1 || stats.Normal[0] != "node-a" {
		t.Fatalf("正常节点不正确: %v", stats.Normal)
	}
	if len(stats.Abnormal) != 1 || stats.Abnormal[0].Name != "node-b" || stats.Abnormal[0].Reason != "KubeletNotReady" {
		t.Fatalf("异常节点不正确: %+v", stats.Abnormal)
	}
}

func TestSummarizePods(t *testing.T) {
	pods := []corev1.Pod{
		testPod("default", "web-1", corev1.PodRunning,
			[]corev1.ContainerStatus{mainContainer("web", true, 0, runningState())}),
		testPod("default", "crash-1", corev1.PodRunning,
			[]corev1.ContainerStatus{mainContainer("app", false, 7, waitingState("CrashLoopBackOff"))}),
	}

	var infos []collect.PodInspection
	for i := range pods {
		infos = append(infos, collect.InspectPod(&pods[i]))
	}
	stats := SummarizePods(infos)

	if stats.Total != 2 || stats.Abnormal != 1 {
		t.Fatalf("统计不正确: Total=%d Abnormal=%d", stats.Total, stats.Abnormal)
	}
	if stats.TotalRestarts != 7 {
		t.Fatalf("重启总次数 = %d, want 7", stats.TotalRestarts)
	}
	if len(stats.AbnormalPods) != 1 || stats.AbnormalPods[0].Name != "crash-1" {
		t.Fatalf("异常 Pod 不正确: %+v", stats.AbnormalPods)
	}
	if len(stats.NormalPods) != 1 || stats.NormalPods[0].Name != "web-1" {
		t.Fatalf("正常 Pod 不正确: %+v", stats.NormalPods)
	}
}

func TestPrintWithAbnormal(t *testing.T) {
	nodes := []corev1.Node{
		testNode("k3s-control-1", corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue}),
		testNode("k3s-worker-1", corev1.NodeCondition{
			Type: corev1.NodeReady, Status: corev1.ConditionFalse, Reason: "KubeletNotReady",
			Message: "container runtime is down",
		}),
	}
	pods := []corev1.Pod{
		testPod("default", "web-1", corev1.PodRunning,
			[]corev1.ContainerStatus{mainContainer("web", true, 0, runningState())}),
		testPod("default", "crash-1", corev1.PodRunning,
			[]corev1.ContainerStatus{mainContainer("app", false, 7, waitingState("CrashLoopBackOff"))}),
	}

	var buf bytes.Buffer
	Print(&buf, Build("default", nodes, pods, Options{}), DisplayOptions{})
	out := buf.String()

	for _, want := range []string{
		"Kubernetes 集群巡检报告",
		"巡检范围: Pod default",
		"【节点】总数 2, Ready 1, NotReady 1",
		"正常节点: k3s-control-1",
		"异常节点:",
		"k3s-worker-1", "KubeletNotReady",
		"【Pod】总数 2, 异常 1, 重启总次数 7",
		"异常 Pod:",
		"crash-1", "CrashLoopBackOff",
		"正常 Pod:",
		"web-1",
		"【结论】发现 2 个异常资源（异常节点 1 个，异常 Pod 1 个）",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出中缺少 %q\n实际输出:\n%s", want, out)
		}
	}
}

func TestPrintHealthy(t *testing.T) {
	nodes := []corev1.Node{
		testNode("node-a", corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue}),
	}
	pods := []corev1.Pod{
		testPod("default", "web-1", corev1.PodRunning,
			[]corev1.ContainerStatus{mainContainer("web", true, 0, runningState())}),
	}

	var buf bytes.Buffer
	Print(&buf, Build("", nodes, pods, Options{}), DisplayOptions{})
	out := buf.String()

	if !strings.Contains(out, "巡检范围: Pod 全部 Namespace") {
		t.Errorf("巡检范围提示不正确:\n%s", out)
	}
	if !strings.Contains(out, "巡检正常，未发现异常资源") {
		t.Errorf("没有异常时应当明确显示巡检正常:\n%s", out)
	}
	if strings.Contains(out, "异常 Pod:") || strings.Contains(out, "异常节点:") {
		t.Errorf("没有异常时不应当出现异常明细:\n%s", out)
	}
}

func TestPrintEmpty(t *testing.T) {
	var buf bytes.Buffer
	Print(&buf, Build("", nil, nil, Options{}), DisplayOptions{})
	out := buf.String()

	for _, want := range []string{"没有发现任何节点", "未发现任何 Pod", "巡检正常，未发现异常资源"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出中缺少 %q\n实际输出:\n%s", want, out)
		}
	}
}

func TestPrintJSON(t *testing.T) {
	nodes := []corev1.Node{
		testNode("k3s-control-1", corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue}),
		testNode("k3s-worker-1", corev1.NodeCondition{
			Type: corev1.NodeReady, Status: corev1.ConditionFalse, Reason: "KubeletNotReady",
		}),
	}
	pods := []corev1.Pod{
		testPod("default", "web-1", corev1.PodRunning,
			[]corev1.ContainerStatus{mainContainer("web", true, 0, runningState())}),
		testPod("default", "crash-1", corev1.PodRunning,
			[]corev1.ContainerStatus{mainContainer("app", false, 7, waitingState("CrashLoopBackOff"))}),
	}

	var buf bytes.Buffer
	if err := PrintJSON(&buf, Build("default", nodes, pods, Options{})); err != nil {
		t.Fatalf("JSON 编码失败: %v", err)
	}

	// 往返校验：unmarshal 回来，字段值必须与内存中的报告一致。
	var got Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("输出的 JSON 无法解析: %v\n内容:\n%s", err, buf.String())
	}

	if got.Nodes.Total != 2 || got.Nodes.Ready != 1 || got.Nodes.NotReady != 1 {
		t.Fatalf("节点统计不正确: %+v", got.Nodes)
	}
	if len(got.Nodes.Abnormal) != 1 || got.Nodes.Abnormal[0].Name != "k3s-worker-1" ||
		got.Nodes.Abnormal[0].Reason != "KubeletNotReady" {
		t.Fatalf("异常节点不正确: %+v", got.Nodes.Abnormal)
	}
	if got.Pods.Total != 2 || got.Pods.Abnormal != 1 || got.Pods.TotalRestarts != 7 {
		t.Fatalf("Pod 统计不正确: %+v", got.Pods)
	}
	if len(got.Pods.AbnormalPods) != 1 || got.Pods.AbnormalPods[0].Name != "crash-1" ||
		got.Pods.AbnormalPods[0].Phase != corev1.PodRunning ||
		len(got.Pods.AbnormalPods[0].Problems) != 1 {
		t.Fatalf("异常 Pod 不正确: %+v", got.Pods.AbnormalPods)
	}
	if len(got.Pods.NormalPods) != 1 || got.Pods.NormalPods[0].Name != "web-1" ||
		len(got.Pods.NormalPods[0].Problems) != 0 {
		t.Fatalf("正常 Pod 不正确: %+v", got.Pods.NormalPods)
	}
	if got.GeneratedAt.IsZero() {
		t.Fatal("generatedAt 不应当是零值")
	}
}

func TestPrintJSONHealthy(t *testing.T) {
	nodes := []corev1.Node{
		testNode("node-a", corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue}),
	}

	var buf bytes.Buffer
	if err := PrintJSON(&buf, Build("", nodes, nil, Options{})); err != nil {
		t.Fatalf("JSON 编码失败: %v", err)
	}

	var got Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("输出的 JSON 无法解析: %v\n内容:\n%s", err, buf.String())
	}

	// 全健康时：异常列表被 omitempty 省略，必须保持空（不能 unmarshal 成未知值）。
	if got.Nodes.NotReady != 0 || len(got.Nodes.Abnormal) != 0 {
		t.Fatalf("健康集群不应当有异常节点: %+v", got.Nodes)
	}
	if got.Pods.Total != 0 || got.Pods.Abnormal != 0 || len(got.Pods.AbnormalPods) != 0 {
		t.Fatalf("空 Pod 列表统计不正确: %+v", got.Pods)
	}
}

func TestBuildRestartThreshold(t *testing.T) {
	nodes := []corev1.Node{
		testNode("node-a", corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue}),
	}
	pods := []corev1.Pod{
		// 重启 3 次，无其他异常：阈值 2 时应被判异常并带原因，阈值 0 时保持正常
		testPod("default", "oom-restart-1", corev1.PodRunning,
			[]corev1.ContainerStatus{mainContainer("app", true, 3, runningState())}),
		// 重启 1 次：阈值 2 时仍是正常
		testPod("default", "web-1", corev1.PodRunning,
			[]corev1.ContainerStatus{mainContainer("web", true, 1, runningState())}),
	}

	below := Build("default", nodes, pods, Options{RestartThreshold: 2})
	if below.Pods.Abnormal != 1 {
		t.Fatalf("阈值 2 时异常 Pod 数 = %d, want 1", below.Pods.Abnormal)
	}
	if below.Pods.AbnormalPods[0].Name != "oom-restart-1" {
		t.Fatalf("被判异常的 Pod 不正确: %+v", below.Pods.AbnormalPods[0])
	}
	found := false
	for _, p := range below.Pods.AbnormalPods[0].Problems {
		if strings.Contains(p, "达到阈值 2") {
			found = true
		}
	}
	if !found {
		t.Fatalf("异常原因缺少阈值说明: %+v", below.Pods.AbnormalPods[0].Problems)
	}

	off := Build("default", nodes, pods, Options{})
	if off.Pods.Abnormal != 0 {
		t.Fatalf("未启用阈值时异常 Pod 数 = %d, want 0", off.Pods.Abnormal)
	}
}

func TestPrintOnlyAbnormal(t *testing.T) {
	nodes := []corev1.Node{
		testNode("node-a", corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue}),
	}
	pods := []corev1.Pod{
		testPod("default", "web-1", corev1.PodRunning,
			[]corev1.ContainerStatus{mainContainer("web", true, 0, runningState())}),
		testPod("default", "crash-1", corev1.PodRunning,
			[]corev1.ContainerStatus{mainContainer("app", false, 7, waitingState("CrashLoopBackOff"))}),
	}

	var buf bytes.Buffer
	Print(&buf, Build("default", nodes, pods, Options{}), DisplayOptions{OnlyAbnormal: true})
	out := buf.String()

	if !strings.Contains(out, "crash-1") {
		t.Errorf("只显示异常模式仍应输出异常 Pod:/n%s", out)
	}
	if strings.Contains(out, "web-1") || strings.Contains(out, "正常 Pod:") {
		t.Errorf("只显示异常模式不应出现正常 Pod 明细:\n%s", out)
	}
	// 统计口径保持全量
	if !strings.Contains(out, "【Pod】总数 2, 异常 1") {
		t.Errorf("统计数字应当保持全量:\n%s", out)
	}
}
