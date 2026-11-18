// Command k8s-inspector 是一个只读的 Kubernetes 集群巡检工具入口。
//
// 目录结构（Go 标准工程布局）：
//
//	cmd/k8s-inspector/   程序入口，只做启动
//	internal/app/        流程编排
//	internal/config/     命令行参数与 kubeconfig 加载
//	internal/kube/       Kubernetes 客户端创建
//	internal/collect/    节点与 Pod 信息采集、异常判断
//	internal/report/     巡检报告生成（终端渲染）
//
// 所有业务代码都在 internal/ 下，禁止被外部项目 import；后续新增能力
// （JSON 输出、过滤、CRD 等）按职责在 internal/ 下加新包即可。
package main

import (
	"fmt"
	"os"

	"k8s-inspector/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}
