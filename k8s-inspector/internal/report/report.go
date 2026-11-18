// Package report 负责“报告生成”：把 collect 包的采集结果汇总成 Report，
// 并渲染为终端报告。采集与展示分离，这个包不发起任何 Kubernetes API 调用。
//
// TODO 5 的 JSON 输出将复用 Report 数据结构，只新增一种渲染方式。
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	corev1 "k8s.io/api/core/v1"

	"k8s-inspector/internal/collect"
)

// NodeStats 是节点维度的统计结果。
type NodeStats struct {
	Total    int                      `json:"total"`
	Ready    int                      `json:"ready"`
	NotReady int                      `json:"notReady"`
	Normal   []string                 `json:"normal,omitempty"`   // 正常节点名
	Abnormal []collect.NodeInspection `json:"abnormal,omitempty"` // 异常节点明细
}

// PodStats 是 Pod 维度的统计结果。
type PodStats struct {
	Total         int                     `json:"total"`
	Abnormal      int                     `json:"abnormal"`
	TotalRestarts int32                   `json:"totalRestarts"`
	AbnormalPods  []collect.PodInspection `json:"abnormalPods,omitempty"`
	NormalPods    []collect.PodInspection `json:"normalPods,omitempty"`
}

// Report 是一次巡检的完整报告数据。
// 采集与展示分离：这个结构只承载数据，展示由 Print（终端文本）或 PrintJSON（JSON）完成。
type Report struct {
	GeneratedAt time.Time `json:"generatedAt"`
	PodScope    string    `json:"podScope"` // 空 表示全部 Namespace
	Nodes       NodeStats `json:"nodes"`
	Pods        PodStats  `json:"pods"`
}

// Options 是报告统计阶段的可调判断规则。阈值属于"用户定义的异常标准"，
// 在报告层追加原因，不改动 collect 对原始 Kubernetes 对象的判断逻辑。
type Options struct {
	// RestartThreshold 重启次数阈值；> 0 时，累计重启达到阈值的 Pod 视为异常。
	RestartThreshold int32
}

// DisplayOptions 控制终端报告的显示方式，不影响 JSON 输出（JSON 始终全量）。
type DisplayOptions struct {
	// OnlyAbnormal 为 true 时终端报告省略正常 Pod 明细。
	OnlyAbnormal bool
}

// Build 采集 + 统计，产出报告数据。
func Build(podScope string, nodes []corev1.Node, pods []corev1.Pod, opts Options) Report {
	r := Report{GeneratedAt: time.Now(), PodScope: podScope}

	nodeInfos := make([]collect.NodeInspection, 0, len(nodes))
	for i := range nodes {
		nodeInfos = append(nodeInfos, collect.InspectNode(&nodes[i]))
	}
	r.Nodes = SummarizeNodes(nodeInfos)

	podInfos := make([]collect.PodInspection, 0, len(pods))
	for i := range pods {
		info := collect.InspectPod(&pods[i])
		applyRestartThreshold(&info, opts.RestartThreshold)
		podInfos = append(podInfos, info)
	}
	r.Pods = SummarizePods(podInfos)

	return r
}

// applyRestartThreshold 在 Pod 累计重启达到阈值时追加一条异常原因。
// 追加到 Problems 后 Abnormal() 自然为 true，统计与展示口径保持一致。
func applyRestartThreshold(info *collect.PodInspection, threshold int32) {
	if threshold <= 0 || info.Restarts < threshold {
		return
	}
	info.Problems = append(info.Problems,
		fmt.Sprintf("重启 %d 次达到阈值 %d", info.Restarts, threshold))
}

// SummarizeNodes 汇总节点巡检结果，正常与异常分开存放。
func SummarizeNodes(infos []collect.NodeInspection) NodeStats {
	stats := NodeStats{Total: len(infos)}
	for _, info := range infos {
		if info.Ready {
			stats.Ready++
			stats.Normal = append(stats.Normal, info.Name)
			continue
		}
		stats.NotReady++
		stats.Abnormal = append(stats.Abnormal, info)
	}
	return stats
}

// SummarizePods 汇总 Pod 巡检结果：总数、异常数、重启总次数，以及正常/异常两组明细。
func SummarizePods(infos []collect.PodInspection) PodStats {
	stats := PodStats{Total: len(infos)}
	for _, info := range infos {
		stats.TotalRestarts += info.Restarts
		if info.Abnormal() {
			stats.Abnormal++
			stats.AbnormalPods = append(stats.AbnormalPods, info)
			continue
		}
		stats.NormalPods = append(stats.NormalPods, info)
	}
	return stats
}

// Print 输出完整的终端报告。
func Print(w io.Writer, r Report, opts DisplayOptions) {
	fmt.Fprintln(w, "==================== Kubernetes 集群巡检报告 ====================")
	fmt.Fprintf(w, "巡检时间: %s\n", r.GeneratedAt.Format("2006-01-02 15:04:05"))
	scope := r.PodScope
	if scope == "" {
		scope = "全部 Namespace"
	}
	fmt.Fprintf(w, "巡检范围: Pod %s\n\n", scope)

	printNodeSection(w, r.Nodes)
	fmt.Fprintln(w)
	printPodSection(w, r.Pods, opts)
	fmt.Fprintln(w)
	printConclusion(w, r)
}

// PrintJSON 以 JSON 格式输出报告，供其他程序（jq、CI 流水线、监控脚本）消费。
// 使用缩进便于人读，EscapeHTML 关闭让中文与符号保持原样。
func PrintJSON(w io.Writer, r Report) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(r)
}

// printNodeSection 输出节点统计与正常/异常节点明细。
func printNodeSection(w io.Writer, stats NodeStats) {
	if stats.Total == 0 {
		fmt.Fprintln(w, "【节点】集群中没有发现任何节点。")
		return
	}
	fmt.Fprintf(w, "【节点】总数 %d, Ready %d, NotReady %d\n", stats.Total, stats.Ready, stats.NotReady)

	if len(stats.Normal) > 0 {
		fmt.Fprintf(w, "正常节点: %s\n", strings.Join(stats.Normal, ", "))
	}
	if len(stats.Abnormal) == 0 {
		return
	}

	fmt.Fprintln(w, "异常节点:")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  NAME\tREASON")
	for _, info := range stats.Abnormal {
		fmt.Fprintf(tw, "  %s\t%s\n", info.Name, info.Reason)
	}
	tw.Flush()
}

// printPodSection 输出 Pod 统计与正常/异常 Pod 明细；OnlyAbnormal 时省略正常 Pod 表。
func printPodSection(w io.Writer, stats PodStats, opts DisplayOptions) {
	if stats.Total == 0 {
		fmt.Fprintln(w, "【Pod】未发现任何 Pod。")
		return
	}
	fmt.Fprintf(w, "【Pod】总数 %d, 异常 %d, 重启总次数 %d\n", stats.Total, stats.Abnormal, stats.TotalRestarts)

	if len(stats.AbnormalPods) > 0 {
		fmt.Fprintln(w, "异常 Pod:")
		printPodTable(w, stats.AbnormalPods, true)
	}
	if opts.OnlyAbnormal {
		// 统计数字仍然全量，只是省略正常明细；确保"只看异常"不改变数据口径。
		return
	}
	if len(stats.NormalPods) > 0 {
		fmt.Fprintln(w, "正常 Pod:")
		printPodTable(w, stats.NormalPods, false)
	}
}

// printPodTable 输出 Pod 明细表格；withProblems 控制是否包含 PROBLEM 列（异常表需要，正常表不需要）。
func printPodTable(w io.Writer, infos []collect.PodInspection, withProblems bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := "  NAMESPACE\tNAME\tPHASE\tREADY\tIP\tRESTARTS"
	if withProblems {
		header += "\tPROBLEM"
	}
	fmt.Fprintln(tw, header)

	for _, info := range infos {
		podIP := info.PodIP
		if podIP == "" {
			podIP = "-"
		}
		line := fmt.Sprintf("  %s\t%s\t%s\t%s\t%s\t%d",
			info.Namespace, info.Name, info.Phase, info.ReadyText(), podIP, info.Restarts)
		if withProblems {
			problem := strings.Join(info.Problems, "; ")
			if problem == "" {
				problem = "-"
			}
			line += "\t" + problem
		}
		fmt.Fprintln(tw, line)
	}
	tw.Flush()
}

// printConclusion 输出结论：有异常列出数量，没有异常明确说明巡检正常。
func printConclusion(w io.Writer, r Report) {
	total := r.Nodes.NotReady + r.Pods.Abnormal
	if total == 0 {
		fmt.Fprintln(w, "【结论】巡检正常，未发现异常资源。")
		return
	}
	fmt.Fprintf(w, "【结论】发现 %d 个异常资源（异常节点 %d 个，异常 Pod %d 个），详见上方明细。\n",
		total, r.Nodes.NotReady, r.Pods.Abnormal)
}
