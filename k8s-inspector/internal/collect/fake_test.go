// fake_test.go：使用 client-go 的 Fake Clientset 测试采集函数，
// 验证 ListNodes / ListPods 的真实调用链（构造请求 -> 反序列化 -> 返回切片），
// 全程不连接任何集群。
package collect

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// fakeNode / fakePod 构造最小可入库的 Kubernetes 对象（fake client 按对象存储）。
func fakeNode(name string, ready corev1.ConditionStatus) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: ready},
		}},
	}
}

func fakePod(namespace, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func TestListNodesWithFakeClient(t *testing.T) {
	t.Run("空集群返回空切片且不报错", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		nodes, err := ListNodes(context.Background(), clientset)
		if err != nil {
			t.Fatalf("空集群不应报错: %v", err)
		}
		if len(nodes) != 0 {
			t.Fatalf("空集群节点数 = %d, want 0", len(nodes))
		}
	})

	t.Run("返回全部节点", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(
			fakeNode("node-a", corev1.ConditionTrue),
			fakeNode("node-b", corev1.ConditionFalse),
		)
		nodes, err := ListNodes(context.Background(), clientset)
		if err != nil {
			t.Fatalf("查询节点失败: %v", err)
		}
		if len(nodes) != 2 {
			t.Fatalf("节点数 = %d, want 2", len(nodes))
		}
		// 采集结果能直接进入判断逻辑：Ready 状态与入库时一致
		readyA, _ := NodeReadyStatus(&nodes[0])
		readyB, _ := NodeReadyStatus(&nodes[1])
		if (nodes[0].Name == "node-a" && (!readyA || readyB)) || (nodes[0].Name == "node-b" && (!readyB || readyA)) {
			t.Fatalf("Ready 状态与入库数据不一致: node-a=%v node-b=%v", readyA, readyB)
		}
	})
}

func TestListPodsWithFakeClient(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		fakePod("default", "web-1"),
		fakePod("default", "web-2"),
		fakePod("kube-system", "coredns-x"),
	)

	t.Run("全部 Namespace 返回所有 Pod", func(t *testing.T) {
		pods, err := ListPods(context.Background(), clientset, "")
		if err != nil {
			t.Fatalf("查询全部 Namespace 失败: %v", err)
		}
		if len(pods) != 3 {
			t.Fatalf("Pod 数 = %d, want 3", len(pods))
		}
	})

	t.Run("指定 Namespace 只返回该 Namespace 的 Pod", func(t *testing.T) {
		pods, err := ListPods(context.Background(), clientset, "default")
		if err != nil {
			t.Fatalf("查询 default Namespace 失败: %v", err)
		}
		if len(pods) != 2 {
			t.Fatalf("Pod 数 = %d, want 2", len(pods))
		}
		for _, p := range pods {
			if p.Namespace != "default" {
				t.Fatalf("查询 default 却返回了 %s/%s", p.Namespace, p.Name)
			}
		}
	})

	t.Run("不存在的 Namespace 返回空切片且不报错", func(t *testing.T) {
		pods, err := ListPods(context.Background(), clientset, "no-such-ns")
		if err != nil {
			t.Fatalf("查询不存在的 Namespace 不应报错: %v", err)
		}
		if len(pods) != 0 {
			t.Fatalf("Pod 数 = %d, want 0", len(pods))
		}
	})
}

func TestListPodsEmptyClusterWithFakeClient(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	pods, err := ListPods(context.Background(), clientset, "")
	if err != nil {
		t.Fatalf("空集群不应报错: %v", err)
	}
	if len(pods) != 0 {
		t.Fatalf("空集群 Pod 数 = %d, want 0", len(pods))
	}
}

// TestFakeClientSurfacesErrors 验证错误路径：fake client 注入 reactor 模拟 API 失败，
// 确认 ListNodes 会把错误包装成带上下文的巡检错误（而不是裸的 API 错误）。
func TestFakeClientSurfacesErrors(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("list", "nodes", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.DeadlineExceeded
	})

	_, err := ListNodes(context.Background(), clientset)
	if err == nil {
		t.Fatal("API 失败时应当返回错误")
	}
	if !strings.Contains(err.Error(), "获取节点列表失败") {
		t.Fatalf("错误信息缺少巡检上下文: %v", err)
	}
}
