// Package app 负责一次巡检的完整流程编排：
// 解析参数 -> 加载 kubeconfig -> 创建客户端 -> 采集数据 -> 生成报告 -> 输出。
// 这个包只做编排顺序，不做任何具体工作。
package app

import (
	"context"
	"io"

	"k8s-inspector/internal/collect"
	"k8s-inspector/internal/config"
	"k8s-inspector/internal/kube"
	"k8s-inspector/internal/report"
)

// Run 执行一次完整巡检，结果写入 stdout，错误信息写入 stderr。
func Run(args []string, stdout, stderr io.Writer) error {
	cfg, err := config.Parse(args, stderr)
	if err != nil {
		return err
	}

	restConfig, err := config.BuildRESTConfig(cfg.Kubeconfig)
	if err != nil {
		return err
	}

	clientset, err := kube.NewClientset(restConfig)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	nodes, err := collect.ListNodes(ctx, clientset)
	if err != nil {
		return err
	}
	if cfg.NodeName != "" {
		// 节点过滤在统计之前完成；指定节点不存在时直接报错，避免"静默巡检了 0 个节点"。
		nodes, err = collect.SelectNode(nodes, cfg.NodeName)
		if err != nil {
			return err
		}
	}

	pods, err := collect.ListPods(ctx, clientset, cfg.Namespace)
	if err != nil {
		return err
	}

	built := report.Build(cfg.Namespace, nodes, pods, report.Options{
		RestartThreshold: cfg.RestartThreshold,
	})

	switch cfg.Output {
	case config.OutputJSON:
		// JSON 格式把编码错误也当作巡检失败处理，避免静默输出不完整的报告。
		return report.PrintJSON(stdout, built)
	default:
		report.Print(stdout, built, report.DisplayOptions{OnlyAbnormal: cfg.OnlyAbnormal})
		return nil
	}
}
