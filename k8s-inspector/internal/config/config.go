// Package config 负责命令行参数解析与 kubeconfig 加载。
package config

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// OutputFormat 是报告的输出格式。
type OutputFormat string

// 支持的输出格式：text 是人类可读的终端报告，json 供其他程序消费。
const (
	OutputText OutputFormat = "text"
	OutputJSON OutputFormat = "json"
)

// Config 是一次巡检的全部用户输入。
type Config struct {
	Kubeconfig       string        // kubeconfig 路径；空 表示走默认查找规则
	Namespace        string        // Pod 巡检范围；空 表示全部 Namespace
	Timeout          time.Duration // 访问 apiserver 的超时时间
	Output           OutputFormat  // 报告输出格式，默认 text
	OnlyAbnormal     bool          // 终端报告只显示异常 Pod（不影响 JSON 输出）
	RestartThreshold int32         // 重启次数阈值；Pod 累计重启达到该值视为异常，0 表示不启用
	NodeName         string        // 只巡检指定节点；空 表示全部节点
}

// Parse 解析命令行参数。不引入 Cobra 等框架，标准库 flag 够用；
// 后续参数增多时再考虑拆子命令。
func Parse(args []string, stderr io.Writer) (*Config, error) {
	fs := flag.NewFlagSet("k8s-inspector", flag.ContinueOnError)
	fs.SetOutput(stderr)
	kubeconfig := fs.String("kubeconfig", "", "kubeconfig 文件路径；留空则按 KUBECONFIG 环境变量、~/.kube/config 的顺序查找")
	namespace := fs.String("namespace", "", "只巡检指定 Namespace 的 Pod；留空表示全部 Namespace")
	timeout := fs.Duration("timeout", 10*time.Second, "访问 apiserver 的超时时间")
	output := fs.String("output", string(OutputText), fmt.Sprintf("报告输出格式: %s 或 %s", OutputText, OutputJSON))
	onlyAbnormal := fs.Bool("abnormal-only", false, "终端报告只显示异常 Pod（JSON 输出始终包含全量数据）")
	restartThreshold := fs.Int("restart-threshold", 0, "重启次数阈值；Pod 累计重启达到该次数视为异常，0 表示不启用")
	nodeName := fs.String("node", "", "只巡检指定的节点；留空表示检查全部节点")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if *output != string(OutputText) && *output != string(OutputJSON) {
		return nil, fmt.Errorf("不支持的输出格式 %q，可选值: text、json", *output)
	}
	if *restartThreshold < 0 {
		return nil, fmt.Errorf("重启次数阈值不能为负数，当前值: %d", *restartThreshold)
	}

	return &Config{
		Kubeconfig:       *kubeconfig,
		Namespace:        *namespace,
		Timeout:          *timeout,
		Output:           OutputFormat(*output),
		OnlyAbnormal:     *onlyAbnormal,
		RestartThreshold: int32(*restartThreshold),
		NodeName:         *nodeName,
	}, nil
}

// BuildRESTConfig 只负责“从哪里读、怎么读”kubeconfig，不把任何路径写死在业务逻辑里。
// 优先使用显式传入的路径，否则交给 client-go 的标准查找规则（KUBECONFIG 环境变量、默认家目录）。
func BuildRESTConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("读取 kubeconfig %q 失败: %w", kubeconfigPath, err)
		}
		return restConfig, nil
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		defaultPath := loadingRules.GetDefaultFilename()
		if _, statErr := os.Stat(defaultPath); statErr != nil {
			return nil, fmt.Errorf("未找到 kubeconfig 文件 %q\n"+
				"提示: 可通过 KUBECONFIG 环境变量或 -kubeconfig 参数指定集群配置文件: %w", defaultPath, err)
		}
		return nil, fmt.Errorf("解析 kubeconfig %q 失败: %w", defaultPath, err)
	}
	return restConfig, nil
}
