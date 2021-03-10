/**
 * Copyright (c) 2020 CoCreate LLC
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy of
 * this software and associated documentation files (the "Software"), to deal in
 * the Software without restriction, including without limitation the rights to
 * use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
 * the Software, and to permit persons to whom the Software is furnished to do so,
 * subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
 * FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
 * COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
 * IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
 * CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */

package autoscaler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/CoCreate-app/CoCreateLB/nodeautoscaler/pkg/config"
	"github.com/CoCreate-app/CoCreateLB/nodeautoscaler/pkg/controller"
	mcalc "github.com/CoCreate-app/CoCreateLB/nodeautoscaler/pkg/metriccalc"
	ms "github.com/CoCreate-app/CoCreateLB/nodeautoscaler/pkg/metricsource"
	pv "github.com/CoCreate-app/CoCreateLB/nodeautoscaler/pkg/provisioner"
	"github.com/CoCreate-app/CoCreateLB/nodeautoscaler/pkg/util"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

var logger = klogr.New().WithName("autoscaler")

// AutoScaler is the main entity managing auto scaling
type AutoScaler struct {
	mainConfig config.Config
	// controller watches node resource in Kubernetes
	kubeNodeController *controller.KubeNodeController
	// put clientset here to support controller, general client, and leader election
	clientset *kubernetes.Clientset
	// keys of available node involved in metrics calculation
	// only ready nodes are included
	availNodes map[string]*v1.Node
	// lock used to update availNodes
	rwlock sync.RWMutex
	// metrics calculator to evaluate when to scale
	calculator *mcalc.Calculator
	// metric source
	metricSource ms.MetricSource
	// backend provisioner
	provisioner pv.Provisioner

	context context.Context

	// cancel func for lower context
	lowerCancel context.CancelFunc
	// used by lower component for notifying shutdown
	reverseCloseCh chan struct{}
}

// NewAutoScaler creates an AutoScaler instance
func NewAutoScaler(ctx context.Context, cfg config.Config) (*AutoScaler, error) {
	as := AutoScaler{
		mainConfig:     cfg,
		availNodes:     make(map[string]*v1.Node, 4),
		rwlock:         sync.RWMutex{},
		context:        ctx,
		reverseCloseCh: make(chan struct{}),
	}

	lowerCtx, cancel := context.WithCancel(ctx)

	as.lowerCancel = cancel

	var err error
	as.clientset, err = createKubeClient(cfg.KubeConfigFile)
	if err != nil {
		return nil, err
	}

	as.kubeNodeController, err = controller.NewController(lowerCtx, cfg, as.clientset, (&as).updateAvailNodes)
	if err != nil {
		logger.Error(err, "failed to create node controller")
		return nil, err
	}

	as.metricSource, err = createMetricSource(lowerCtx, cfg)
	if err != nil {
		logger.Error(err, "failed to create metric source", "metric source", cfg.MetricSource)
		return nil, err
	}

	as.provisioner, err = createProvisioner(cfg)
	if err != nil {
		logger.Error(err, "failed to create backend provisioner", "provisioner", cfg.BackendProvsioner)
		return nil, err
	}

	as.calculator, err = mcalc.NewCalculator(lowerCtx, as.reverseCloseCh, as.availNodes, &(as.rwlock),
		as.metricSource, as.provisioner, cfg)
	if err != nil {
		logger.Error(err, "failed to create metric calculator")
		return nil, err
	}

	return &as, nil
}

// Run runs the main processing
func (a *AutoScaler) Run() {
	defer runtime.HandleCrash()
	defer klog.Flush()
	var wg sync.WaitGroup
	defer wg.Wait()
	// must be put below defer wg.Wait()
	defer a.lowerCancel()

	logger.Info("starting auto scaler")

	go a.kubeNodeController.Run(&wg)
	go a.calculator.Run(&wg)

	select {
	case <-a.context.Done():
	case <-a.reverseCloseCh:
	}
	logger.Info("stoping auto scaler")
}

func createKubeClient(kubeconfig string) (*kubernetes.Clientset, error) {
	var clientset *kubernetes.Clientset
	config, err := util.CreateRestCfg(kubeconfig)
	if err != nil {
		logger.Error(err, "failed to create Kubernetes client")
		return nil, err
	}
	if kubeconfig != "" {
		clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			logger.Error(err, "failed to create kubeclient from kubeconfig", "kubeconfig", kubeconfig)
			return nil, err
		}
	} else {
		clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			logger.Error(err, "failed to create kubeclient from in-cluster config")
			return nil, err
		}
	}

	return clientset, nil
}

func (a *AutoScaler) updateAvailNodes(key string) error {
	defer klog.Flush()

	logger.V(3).Info("call updateAvailNodes", "key", key)

	var addNode, delNode bool
	var nodeObj *v1.Node

	obj, exists, err := a.kubeNodeController.GetByKey(key)
	if err != nil {
		logger.Error(err, "failed to get node object from cache by key", "node key", key)
		return err
	}

	if !exists {
		logger.Info("Node does not exist anymore", "node key", key)
		_, delNode = a.ifUpdateNodes(false, key)
		addNode = false
	} else {
		nodeObj, ok := obj.(*v1.Node)
		if !ok {
			err := fmt.Errorf("returned object is not a node")
			logger.Error(err, err.Error(), "node key")
			return err
		}
		nodeReady := isNodeReady(nodeObj)

		addNode, delNode = a.ifUpdateNodes(nodeReady, key)
	}

	if (!addNode) && (!delNode) {
		return nil
	}

	a.rwlock.Lock()
	logger.V(4).Info("got write lock", "key", key)
	defer a.rwlock.Unlock()

	if delNode {
		logger.Info("remove unavailable node out of metrics calculation", "node key", key)
		delete(a.availNodes, key)
	}
	if addNode {
		logger.Info("add new avaialbe node into metrics calculation", "node key", key)
		a.availNodes[key] = nodeObj
	}

	return nil
}

func (a *AutoScaler) ifUpdateNodes(nodeReady bool, key string) (addNode, delNode bool) {
	addNode, delNode = false, false
	a.rwlock.RLock()
	defer a.rwlock.RUnlock()
	if _, ok := a.availNodes[key]; ok {
		if !nodeReady {
			delNode = true
		}
	} else if nodeReady {
		addNode = true
	}
	return addNode, delNode
}

var nodeReadyCheckMap = map[v1.NodeConditionType]v1.ConditionStatus{
	v1.NodeReady:              v1.ConditionTrue,
	v1.NodeMemoryPressure:     v1.ConditionFalse,
	v1.NodeDiskPressure:       v1.ConditionFalse,
	v1.NodePIDPressure:        v1.ConditionFalse,
	v1.NodeNetworkUnavailable: v1.ConditionFalse,
}

// node is considered as ready only when all above conditons are matched
func isNodeReady(obj *v1.Node) bool {
	for _, con := range obj.Status.Conditions {
		if st, ok := nodeReadyCheckMap[con.Type]; ok {
			if con.Status != st {
				return false
			}
		}
	}
	return true
}

func createMetricSource(ctx context.Context, cfg config.Config) (ms.MetricSource, error) {
	switch cfg.MetricSource {
	case string(ms.MetricSourceKube):
		restCfg, err := util.CreateRestCfg(cfg.KubeConfigFile)
		if err != nil {
			return nil, err
		}
		ms, err := ms.NewKubeMetricSource(ctx, restCfg, time.Duration(cfg.MetricCacheExpireTime)*time.Second, cfg.LabelSelector)
		if err != nil {
			return nil, err
		}
		return ms, nil
	default:
		return nil, fmt.Errorf("unkown metric source %s", cfg.MetricSource)
	}
}

func createProvisioner(cfg config.Config) (pv.Provisioner, error) {
	switch cfg.BackendProvsioner {
	case string(pv.ProvisionerRancherNodePool):
		proCfg := pv.InternalConfig{
			RancherURL:        cfg.RancherURL,
			RancherToken:      cfg.RancherToken,
			RancherNodePoolID: cfg.RancherNodePodID,
			RancherCA:         cfg.RancherCA,
		}
		p, err := pv.NewProvisionerRancherNodePool(proCfg)
		if err != nil {
			return nil, err
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unkown backend provisioner %s", cfg.BackendProvsioner)
	}
}
