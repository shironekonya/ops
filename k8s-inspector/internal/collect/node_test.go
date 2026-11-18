// node_test.go：节点采集与 Ready 状态判断的单元测试（不依赖真实集群）。
package collect

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func nodeCondition(condType corev1.NodeConditionType, status corev1.ConditionStatus, reason, message string) corev1.NodeCondition {
	return corev1.NodeCondition{Type: condType, Status: status, Reason: reason, Message: message}
}

func testNode(name string, conditions ...corev1.NodeCondition) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status:     corev1.NodeStatus{Conditions: conditions},
	}
}

func TestNodeReadyStatus(t *testing.T) {
	tests := []struct {
		name       string
		node       *corev1.Node
		wantReady  bool
		wantReason string
	}{
		{
			name:      "Ready=True",
			node:      testNode("n1", nodeCondition(corev1.NodeReady, corev1.ConditionTrue, "", "")),
			wantReady: true,
		},
		{
			name:       "Ready=False 带上原因",
			node:       testNode("n2", nodeCondition(corev1.NodeReady, corev1.ConditionFalse, "KubeletNotReady", "runtime down")),
			wantReady:  false,
			wantReason: "KubeletNotReady (runtime down)",
		},
		{
			name:       "Ready=Unknown 且无 Reason 时回落到状态文本",
			node:       testNode("n3", nodeCondition(corev1.NodeReady, corev1.ConditionUnknown, "", "")),
			wantReady:  false,
			wantReason: "Ready=Unknown",
		},
		{
			name:       "只有其他 Condition、没有 NodeReady",
			node:       testNode("n4", nodeCondition(corev1.NodeMemoryPressure, corev1.ConditionFalse, "", "")),
			wantReady:  false,
			wantReason: "节点未上报 Ready 条件",
		},
		{
			name:       "完全没有 Conditions",
			node:       testNode("n5"),
			wantReady:  false,
			wantReason: "节点未上报 Ready 条件",
		},
		{
			name: "NodeReady 在多项 Condition 中间",
			node: testNode("n6",
				nodeCondition(corev1.NodeMemoryPressure, corev1.ConditionFalse, "", ""),
				nodeCondition(corev1.NodeReady, corev1.ConditionTrue, "", ""),
				nodeCondition(corev1.NodeDiskPressure, corev1.ConditionFalse, "", "")),
			wantReady: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready, reason := NodeReadyStatus(tt.node)
			if ready != tt.wantReady {
				t.Fatalf("NodeReadyStatus() ready = %v, want %v", ready, tt.wantReady)
			}
			if tt.wantReason != "" && !strings.Contains(reason, tt.wantReason) {
				t.Fatalf("reason = %q, 期望包含 %q", reason, tt.wantReason)
			}
		})
	}
}

func TestInspectNode(t *testing.T) {
	node := testNode("worker-1", nodeCondition(corev1.NodeReady, corev1.ConditionFalse, "KubeletNotReady", ""))

	info := InspectNode(node)
	if info.Name != "worker-1" {
		t.Fatalf("Name = %q, want %q", info.Name, "worker-1")
	}
	if info.Ready {
		t.Fatal("Ready = true, want false")
	}
	if info.Reason == "" {
		t.Fatal("非 Ready 时 Reason 不应为空")
	}
}

func TestSelectNode(t *testing.T) {
	nodes := []corev1.Node{
		*testNode("worker-1"),
		*testNode("worker-2"),
	}

	got, err := SelectNode(nodes, "worker-2")
	if err != nil {
		t.Fatalf("SelectNode() 返回错误: %v", err)
	}
	if len(got) != 1 || got[0].Name != "worker-2" {
		t.Fatalf("SelectNode() = %v, want [worker-2]", got)
	}

	if _, err := SelectNode(nodes, "not-exist"); err == nil {
		t.Fatal("指定不存在的节点时应返回错误")
	} else if !strings.Contains(err.Error(), "not-exist") {
		t.Fatalf("错误信息应包含节点名，实际: %v", err)
	}

	if _, err := SelectNode(nil, "worker-1"); err == nil {
		t.Fatal("空节点列表时应返回错误")
	}
}
