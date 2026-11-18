// Package collect 负责“信息采集与异常判断”：节点与 Pod 的列表获取（只读），
// 以及把 Kubernetes 原始对象转换为巡检结果结构。
package collect

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ListNodes 只读取节点列表，不做任何写操作。
func ListNodes(ctx context.Context, clientset kubernetes.Interface) ([]corev1.Node, error) {
	nodeList, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取节点列表失败（请检查集群是否可达、当前用户在 kubeconfig 中是否有 list nodes 权限）: %w", err)
	}
	return nodeList.Items, nil
}

// SelectNode 过滤出指定名称的节点。返回新切片，不修改传入切片与任何节点对象；
// 指定节点不存在时返回错误，让调用方给出明确提示。
func SelectNode(nodes []corev1.Node, name string) ([]corev1.Node, error) {
	var filtered []corev1.Node
	for i := range nodes {
		if nodes[i].Name == name {
			filtered = append(filtered, nodes[i])
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("集群中不存在名为 %q 的节点（可先运行不带 -node 的巡检查看全部节点名称）", name)
	}
	return filtered, nil
}

// NodeInspection 是一个节点的巡检结果。
// JSON 标签供 -output json 使用，字段命名保持小驼峰，便于被其他程序（jq、CI 等）消费。
type NodeInspection struct {
	Name   string `json:"name"`
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"` // 非 Ready 时的原因
}

// InspectNode 采集单个节点的 Ready 状态。
func InspectNode(node *corev1.Node) NodeInspection {
	ready, reason := NodeReadyStatus(node)
	return NodeInspection{Name: node.Name, Ready: ready, Reason: reason}
}

// NodeReadyStatus 从 Node 的 Conditions 中判断 Ready 状态。
// 返回 (是否 Ready, 非 Ready 时的原因)。
//
// 注意：Ready 只是 Conditions 中的一项，不能用“有没有 Conditions”来代替判断；
// 也必须在 Conditions 里显式找到 NodeReady，找不到时按未就绪处理并说明原因。
func NodeReadyStatus(node *corev1.Node) (bool, string) {
	for _, condition := range node.Status.Conditions {
		if condition.Type != corev1.NodeReady {
			continue
		}
		if condition.Status == corev1.ConditionTrue {
			return true, ""
		}
		reason := condition.Reason
		if reason == "" {
			reason = fmt.Sprintf("Ready=%s", condition.Status)
		}
		if condition.Message != "" {
			reason = fmt.Sprintf("%s (%s)", reason, condition.Message)
		}
		return false, reason
	}
	return false, "节点未上报 Ready 条件"
}
