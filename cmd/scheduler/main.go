package main

import (
	"os"

	"k8s.io/component-base/cli"
	_ "k8s.io/component-base/logs/json/register"
	_ "k8s.io/component-base/metrics/prometheus/clientgo"
	_ "k8s.io/component-base/metrics/prometheus/version"
	"k8s.io/kubernetes/cmd/kube-scheduler/app"

	// Import your plugin package
	"github.com/mestrado/scheduler/pkg/reliabilityscheduler"
)

func main() {
	// Register the ReliabilityScheduler plugin
	command := app.NewSchedulerCommand(
		app.WithPlugin(reliabilityscheduler.Name, reliabilityscheduler.New),
	)

	code := cli.Run(command)
	os.Exit(code)
}
