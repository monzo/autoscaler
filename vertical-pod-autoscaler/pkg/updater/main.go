/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"time"

	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/target"

	kube_flag "k8s.io/apiserver/pkg/util/flag"
	"k8s.io/autoscaler/vertical-pod-autoscaler/common"
	vpa_clientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
	updater "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/updater/logic"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/utils/metrics"
	metrics_updater "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/utils/metrics/updater"
	vpa_api_util "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/utils/vpa"
	"k8s.io/client-go/informers"
	kube_client "k8s.io/client-go/kubernetes"
	kube_restclient "k8s.io/client-go/rest"
	"k8s.io/klog"
)

var (
	updaterInterval = flag.Duration("updater-interval", 1*time.Minute,
		`How often updater should run`)

	minReplicas = flag.Int("min-replicas", 2,
		`Minimum number of replicas to perform update`)

	evictionToleranceFraction = flag.Float64("eviction-tolerance", 0.5,
		`Fraction of replica count that can be evicted for update, if more than one pod can be evicted.`)

	evictionRateLimit = flag.Float64("eviction-rate-limit", -1, `
		Number of pods that can be evicted per seconds.`)

	evictionRateLimitBurst = flag.Int("eviction-rate-limit-burst", 1,
		`Burst of pods that can be evicted.`)

	address = flag.String("address", ":8943", "The address to expose Prometheus metrics.")
)

const (
	defaultResyncPeriod time.Duration = 10 * time.Minute
)

func main() {
	kube_flag.InitFlags()
	klog.V(1).Infof("Vertical Pod Autoscaler %s Updater", common.VerticalPodAutoscalerVersion)

	healthCheck := metrics.NewHealthCheck(*updaterInterval*5, true)
	metrics.Initialize(*address, healthCheck)
	metrics_updater.Register()

	config, err := kube_restclient.InClusterConfig()
	if err != nil {
		klog.Fatalf("Failed to build Kubernetes client : fail to create config: %v", err)
	}
	kubeClient := kube_client.NewForConfigOrDie(config)
	vpaClient := vpa_clientset.NewForConfigOrDie(config)
	factory := informers.NewSharedInformerFactory(kubeClient, defaultResyncPeriod)
	targetSelectorFetcher := target.NewCompositeTargetSelectorFetcher(
		target.NewVpaTargetSelectorFetcher(config, kubeClient, factory),
		target.NewBeta1TargetSelectorFetcher(config),
	)
	// TODO: use SharedInformerFactory in updater
	updater, err := updater.NewUpdater(kubeClient, vpaClient, *minReplicas, *evictionRateLimit, *evictionRateLimitBurst, *evictionToleranceFraction, vpa_api_util.NewCappingRecommendationProcessor(), nil, targetSelectorFetcher)
	if err != nil {
		klog.Fatalf("Failed to create updater: %v", err)
	}
	ticker := time.Tick(*updaterInterval)
	for range ticker {
		ctx, cancel := context.WithTimeout(context.Background(), *updaterInterval)
		updater.RunOnce(ctx)
		cancel()
		healthCheck.UpdateLastActivity()
	}
}
