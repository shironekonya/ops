// pod.go：Pod 列表获取与异常判断。所有操作只读。
package collect

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// abnormalWaitingReasons 是容器 Waiting 状态里明确表示“卡住、无法自愈”的原因集合。
// key 统一为小写（标准写法见注释），配合 IsAbnormalWaitingReason 的大小写不敏感查找使用。
var abnormalWaitingReasons = map[string]bool{
	"crashloopbackoff": true, // CrashLoopBackOff
	"imagepullbackoff": true, // ImagePullBackOff
	"errimagepull":     true, // ErrImagePull
	"pending":          true, // Pending
}

// IsAbnormalWaitingReason 判断容器的 Waiting.Reason 是否属于异常。
// 集合的 key 是标准 CamelCase 写法，比较时统一转小写，避免大小写不一致漏判。
func IsAbnormalWaitingReason(reason string) bool {
	return abnormalWaitingReasons[strings.ToLower(reason)]
}

// ListPods 只读取 Pod 列表。namespace 为空时查询全部命名空间。
func ListPods(ctx context.Context, clientset kubernetes.Interface, namespace string) ([]corev1.Pod, error) {
	scope := namespace
	scopeText := fmt.Sprintf("namespace=%s", namespace)
	if scope == "" {
		scope = metav1.NamespaceAll
		scopeText = "全部 Namespace"
	}

	podList, err := clientset.CoreV1().Pods(scope).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取 Pod 列表失败（%s，请检查集群是否可达、当前用户是否有 list pods 权限）: %w", scopeText, err)
	}
	return podList.Items, nil
}

// PodInspection 是一个 Pod 的巡检结果，是 Kubernetes 原始对象与输出之间的中间结构。
// JSON 标签供 -output json 使用，字段命名保持小驼峰。
type PodInspection struct {
	Namespace       string          `json:"namespace"`
	Name            string          `json:"name"`
	Phase           corev1.PodPhase `json:"phase"`
	PodIP           string          `json:"podIP,omitempty"`
	ReadyContainers int             `json:"readyContainers"`
	TotalContainers int             `json:"totalContainers"`
	Restarts        int32           `json:"restarts"`
	Problems        []string        `json:"problems,omitempty"`
}

// Abnormal 表示是否巡检出问题。
func (p PodInspection) Abnormal() bool {
	return len(p.Problems) > 0
}

// ReadyText 返回 就绪容器数/主容器总数，例如 1/2。
func (p PodInspection) ReadyText() string {
	return fmt.Sprintf("%d/%d", p.ReadyContainers, p.TotalContainers)
}

// InspectPod 采集单个 Pod 的关键信息并判断异常。
//
// 判断规则（刻意不把“非 Running”一律当故障）：
//  1. Phase 只把 Pending / Failed / Unknown 视为异常；Succeeded 是正常结束（Job、一次性任务），
//     Running 也不代表健康，需要继续看容器状态。
//  2. 容器 Waiting.Reason 命中 CrashLoopBackOff / ImagePullBackOff / ErrImagePull / Pending 时异常。
//  3. 容器 Terminated.Reason 为 OOMKilled 时异常（包括已经被重启、当前又处于 Running 的容器）。
//     其他退出原因不在这里报：Completed 是正常结束，Error 通常伴随 Phase=Failed 由 Phase 层兜底。
//  4. 重启次数同时累计主容器与 Init 容器；只要发生 OOMKilled 或 CrashLoop，重启次数必然 > 0。
func InspectPod(pod *corev1.Pod) PodInspection {
	info := PodInspection{
		Namespace:       pod.Namespace,
		Name:            pod.Name,
		Phase:           pod.Status.Phase,
		PodIP:           pod.Status.PodIP,
		TotalContainers: len(pod.Spec.Containers),
	}

	switch pod.Status.Phase {
	case corev1.PodPending, corev1.PodFailed, corev1.PodUnknown:
		reason := string(pod.Status.Phase)
		if pod.Status.Reason != "" {
			reason = fmt.Sprintf("%s/%s", reason, pod.Status.Reason)
		}
		if pod.Status.Message != "" {
			reason = fmt.Sprintf("%s: %s", reason, pod.Status.Message)
		}
		info.Problems = append(info.Problems, "Pod "+reason)
	}

	for _, status := range pod.Status.InitContainerStatuses {
		inspectContainerStatus(status, true, &info)
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Ready {
			info.ReadyContainers++
		}
		inspectContainerStatus(status, false, &info)
	}

	return info
}

// inspectContainerStatus 累计重启次数，并把容器的 Waiting / Terminated 异常原因写入 info.Problems。
func inspectContainerStatus(status corev1.ContainerStatus, init bool, info *PodInspection) {
	info.Restarts += status.RestartCount

	name := status.Name
	if init {
		name = "init:" + name
	}

	// 从未启动过的容器（例如镜像一直拉不下来）重启次数是 0，此时不显示，避免噪音。
	restarts := ""
	if status.RestartCount > 0 {
		restarts = fmt.Sprintf("，重启 %d 次", status.RestartCount)
	}

	if waiting := status.State.Waiting; waiting != nil && IsAbnormalWaitingReason(waiting.Reason) {
		info.Problems = append(info.Problems,
			fmt.Sprintf("容器 %s Waiting: %s%s", name, waiting.Reason, restarts))
	}

	if terminated := status.State.Terminated; terminated != nil && terminated.Reason == "OOMKilled" {
		info.Problems = append(info.Problems,
			fmt.Sprintf("容器 %s Terminated: OOMKilled（退出码 %d%s）", name, terminated.ExitCode, restarts))
	}
}
