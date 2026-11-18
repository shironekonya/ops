// Package kube 负责 Kubernetes API 客户端的创建与封装。
package kube

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// NewClientset 根据 rest.Config 构造一个 kubernetes.Clientset。
func NewClientset(restConfig *rest.Config) (*kubernetes.Clientset, error) {
	cs, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("创建 Kubernetes 客户端失败: %w", err)
	}
	return cs, nil
}
