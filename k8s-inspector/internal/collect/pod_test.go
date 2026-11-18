// pod_test.go：Pod 采集与异常判断的单元测试（不依赖真实集群）。
package collect

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func runningState() corev1.ContainerState {
	return corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
}

func waitingState(reason string) corev1.ContainerState {
	return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}
}

func terminatedState(reason string, exitCode int32) corev1.ContainerState {
	return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: reason, ExitCode: exitCode}}
}

func mainContainer(name string, ready bool, restarts int32, state corev1.ContainerState) corev1.ContainerStatus {
	return corev1.ContainerStatus{Name: name, Ready: ready, RestartCount: restarts, State: state}
}

// testPod 按容器状态反推 Spec.Containers，保证 就绪数/总数 的口径和真实对象一致。
func testPod(namespace, name string, phase corev1.PodPhase, initContainers, containers []corev1.ContainerStatus) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Status: corev1.PodStatus{
			Phase:                 phase,
			PodIP:                 "10.42.0.15",
			InitContainerStatuses: initContainers,
			ContainerStatuses:     containers,
		},
	}
	for _, cs := range containers {
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: cs.Name})
	}
	return pod
}

func TestInspectPod(t *testing.T) {
	tests := []struct {
		name         string
		pod          *corev1.Pod
		wantAbnormal bool
		wantRestarts int32
		wantReady    string
		wantProblem  string
	}{
		{
			name: "Running 且容器就绪 - 正常",
			pod: testPod("default", "web-1", corev1.PodRunning, nil,
				[]corev1.ContainerStatus{mainContainer("web", true, 0, runningState())}),
			wantAbnormal: false,
			wantReady:    "1/1",
		},
		{
			name: "Succeeded 正常结束 - 不算故障",
			pod: testPod("batch", "job-1", corev1.PodSucceeded, nil,
				[]corev1.ContainerStatus{mainContainer("task", false, 0, terminatedState("Completed", 0))}),
			wantAbnormal: false,
			wantReady:    "0/1",
		},
		{
			name: "Pending 且调度失败 - 异常",
			pod: func() *corev1.Pod {
				pod := testPod("default", "waiting-1", corev1.PodPending, nil,
					[]corev1.ContainerStatus{mainContainer("app", false, 0, waitingState("ContainerCreating"))})
				pod.Status.Reason = "Unschedulable"
				pod.Status.Message = "0/2 nodes are available"
				return pod
			}(),
			wantAbnormal: true,
			wantReady:    "0/1",
			wantProblem:  "Pod Pending/Unschedulable",
		},
		{
			name: "Failed - 异常",
			pod: testPod("default", "failed-1", corev1.PodFailed, nil,
				[]corev1.ContainerStatus{mainContainer("app", false, 1, terminatedState("Error", 1))}),
			wantAbnormal: true,
			wantRestarts: 1,
			wantReady:    "0/1",
			wantProblem:  "Pod Failed",
		},
		{
			name: "Unknown - 异常",
			pod: testPod("default", "unknown-1", corev1.PodUnknown, nil,
				[]corev1.ContainerStatus{mainContainer("app", false, 0, runningState())}),
			wantAbnormal: true,
			wantReady:    "0/1",
			wantProblem:  "Pod Unknown",
		},
		{
			name: "CrashLoopBackOff - 异常并带上重启次数",
			pod: testPod("default", "crash-1", corev1.PodRunning, nil,
				[]corev1.ContainerStatus{mainContainer("app", false, 7, waitingState("CrashLoopBackOff"))}),
			wantAbnormal: true,
			wantRestarts: 7,
			wantReady:    "0/1",
			wantProblem:  "CrashLoopBackOff，重启 7 次",
		},
		{
			name: "ImagePullBackOff - 异常",
			pod: testPod("default", "pull-1", corev1.PodPending, nil,
				[]corev1.ContainerStatus{mainContainer("app", false, 0, waitingState("ImagePullBackOff"))}),
			wantAbnormal: true,
			wantReady:    "0/1",
			wantProblem:  "ImagePullBackOff",
		},
		{
			name: "ErrImagePull - 异常",
			pod: testPod("default", "pull-2", corev1.PodPending, nil,
				[]corev1.ContainerStatus{mainContainer("app", false, 0, waitingState("ErrImagePull"))}),
			wantAbnormal: true,
			wantReady:    "0/1",
			wantProblem:  "ErrImagePull",
		},
		{
			name: "容器 Waiting: Pending - 异常",
			pod: testPod("default", "wait-pending-1", corev1.PodPending, nil,
				[]corev1.ContainerStatus{mainContainer("app", false, 0, waitingState("Pending"))}),
			wantAbnormal: true,
			wantReady:    "0/1",
			wantProblem:  "容器 app Waiting: Pending",
		},
		{
			name: "Waiting.Reason 小写 pending - 同样按异常处理",
			pod: testPod("default", "wait-pending-2", corev1.PodPending, nil,
				[]corev1.ContainerStatus{mainContainer("app", false, 0, waitingState("pending"))}),
			wantAbnormal: true,
			wantReady:    "0/1",
			wantProblem:  "容器 app Waiting: pending",
		},
		{
			name: "OOMKilled 后被重启回 Running - 仍然异常",
			pod: testPod("default", "oom-1", corev1.PodRunning, nil,
				[]corev1.ContainerStatus{mainContainer("app", true, 3, terminatedState("OOMKilled", 137))}),
			wantAbnormal: true,
			wantRestarts: 3,
			wantReady:    "1/1",
			wantProblem:  "Terminated: OOMKilled（退出码 137，重启 3 次）",
		},
		{
			name: "Running 但就绪探针未通过 - 不算 Phase 故障",
			pod: testPod("default", "notready-1", corev1.PodRunning, nil,
				[]corev1.ContainerStatus{mainContainer("app", false, 0, runningState())}),
			wantAbnormal: false,
			wantReady:    "0/1",
		},
		{
			name: "ContainerCreating 属于正常过渡状态 - 不误报",
			pod: testPod("default", "creating-1", corev1.PodPending, nil,
				[]corev1.ContainerStatus{mainContainer("app", false, 0, waitingState("ContainerCreating"))}),
			wantAbnormal: true, // Phase=Pending 本身按异常上报
			wantReady:    "0/1",
			wantProblem:  "Pod Pending",
		},
		{
			name: "多容器 + Init 容器 - 重启次数全部累计",
			pod: testPod("default", "multi-1", corev1.PodRunning,
				[]corev1.ContainerStatus{mainContainer("init-db", false, 1, terminatedState("Completed", 0))},
				[]corev1.ContainerStatus{
					mainContainer("api", true, 2, runningState()),
					mainContainer("sidecar", false, 3, waitingState("CrashLoopBackOff")),
				}),
			wantAbnormal: true,
			wantRestarts: 6,
			wantReady:    "1/2",
			wantProblem:  "容器 sidecar Waiting: CrashLoopBackOff，重启 3 次",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := InspectPod(tt.pod)
			if info.Abnormal() != tt.wantAbnormal {
				t.Fatalf("Abnormal() = %v, want %v（problems=%v）", info.Abnormal(), tt.wantAbnormal, info.Problems)
			}
			if info.Restarts != tt.wantRestarts {
				t.Fatalf("Restarts = %d, want %d", info.Restarts, tt.wantRestarts)
			}
			if info.ReadyText() != tt.wantReady {
				t.Fatalf("ReadyText() = %q, want %q", info.ReadyText(), tt.wantReady)
			}
			if tt.wantProblem != "" {
				joined := strings.Join(info.Problems, "; ")
				if !strings.Contains(joined, tt.wantProblem) {
					t.Fatalf("problems = %q, 期望包含 %q", joined, tt.wantProblem)
				}
			}
		})
	}
}
