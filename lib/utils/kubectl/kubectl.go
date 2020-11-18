/*
Copyright 2018 Gravitational, Inc.

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

package kubectl

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gravitational/gravity/lib/constants"
	"github.com/gravitational/gravity/lib/defaults"
	"github.com/gravitational/gravity/lib/utils"
	"github.com/gravitational/trace"

	"github.com/davecgh/go-spew/spew"
	log "github.com/sirupsen/logrus"
)

// CreateFromSpec creates resources specified in the provided spec in the given namespace.
// Returns the output of the create command.
func CreateFromSpec(spec []byte, namespace string, args ...string) ([]byte, error) {
	var out []byte
	err := utils.WithTempDir(func(dir string) error {
		path := filepath.Join(dir, "resources")
		err := ioutil.WriteFile(path, spec, defaults.SharedReadMask)
		if err != nil {
			return trace.Wrap(err)
		}

		args = append([]string{"--filename", path, "--namespace", namespace, "--schema-cache-dir", ""}, args...)
		out, err = Create(args...)
		return trace.Wrap(err)
	}, tempDirPrefix)
	if err != nil {
		return nil, trace.Wrap(err, "failed to kubectl create resource from spec: %s", out)
	}
	return out, nil
}

// Delete deletes the specified resourceType.
func Delete(resourceType, name, namespace string) ([]byte, error) {
	return Run("delete", resourceType, name,
		"--namespace", namespace,
		"--ignore-not-found")
}

// Create runs a kubectl create command with the specified arguments
func Create(args ...string) ([]byte, error) {
	return Run("create", args...)
}

// Run runs a kubectl command specified with args using privileged kubeconfig
func Run(name string, args ...string) ([]byte, error) {
	cmd := Command(append([]string{name}, args...)...)
	return RunCommand(cmd, WithPrivilegedConfig())
}

// RunCommand runs a kubectl command specified with args
func RunCommand(cmd *Cmd, options ...optionSetter) ([]byte, error) {
	log.Debugf("executing %v", cmd)
	for _, option := range options {
		option(cmd)
	}

	return exec.Command(cmd.command, cmd.args...).CombinedOutput()
}

func Apply(fileName string) ([]byte, error) {
	/*
		log.Debugf("executing %v", cmd)
		for _, option := range options {
			option(cmd)
		}

		return exec.Command(cmd.command, cmd.args...).CombinedOutput()

		if err := utils.Exec(exec.Command("/bin/bash", "-c", fmt.Sprintf("kubectl apply -f %v", upgradeJobFile)), &kubectlJobOut); err != nil {
			out.WriteString(fmt.Sprintf("Failed volume upgrade k8s exec command. Got output %v:", kubectlJobOut.String()))
			return out.String(), trace.Wrap(err)
		}
	*/

	return Run("apply", "-f", fileName)
}

// GetNamespaces fetches the names of all namespaces
func GetNamespaces(ctx context.Context, runner utils.CommandRunner) ([]string, error) {
	cmd := Command("get", "namespaces", "--output", "jsonpath={.items..metadata.name}")
	var stdout, stderr bytes.Buffer

	err := runner.RunStream(ctx, &stdout, &stderr, cmd.Args()...)
	if err != nil {
		return nil, trace.Wrap(err, "failed to query namespaces: %s", stderr.String())
	}

	namespaces := strings.Fields(strings.TrimSpace(stdout.String()))

	return namespaces, nil
}

// GetPods fetches the names of the pods from the given namespace
func GetPods(ctx context.Context, namespace string, runner utils.CommandRunner) ([]string, error) {
	cmd := Command("get", "pods",
		"--namespace", namespace,
		"--output", "jsonpath={.items..metadata.name}")
	var stdout, stderr bytes.Buffer

	err := runner.RunStream(ctx, &stdout, &stderr, cmd.Args()...)
	if err != nil {
		return nil, trace.Wrap(err, "failed to query pods: %s", stderr.String())
	}

	trimmed := strings.TrimSpace(string(stdout.String()))
	if strings.HasPrefix(trimmed, noResourcesPrefix) {
		return nil, nil
	}

	pods := strings.Fields(trimmed)

	return pods, nil
}

// GetPodContainers fetches the names of the containers from the specified pod
// in the given namespace
func GetPodContainers(ctx context.Context, namespace, pod string, runner utils.CommandRunner) ([]string, error) {
	cmd := Command("get", "pod", pod,
		"--namespace", namespace,
		"--output", "jsonpath={.status.containerStatuses..name}")
	var stdout, stderr bytes.Buffer

	err := runner.RunStream(ctx, &stdout, &stderr, cmd.Args()...)
	if err != nil {
		return nil, trace.Wrap(err, "failed to query containers for pod %v/%v: %s",
			namespace, pod, stderr.String())
	}

	containers := strings.Fields(strings.TrimSpace(stdout.String()))

	return containers, nil
}

// GetNodeAddresses returns internal IP addresses of all nodes in the cluster
func GetNodesAddr(ctx context.Context) ([]string, error) {
	args := utils.PlanetCommand(Command("get", "nodes",
		"--output",
		`jsonpath={.items[*].status.addresses[?(@.type=="InternalIP")].address}`))
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	cmd.Stderr = utils.NewStderrLogger(log.WithField("cmd", "kubectl get nodes"))

	out, err := cmd.Output()
	if err != nil {
		return nil, trace.Wrap(err, "%v : %v", cmd, err)
	}

	nodes := strings.Fields(strings.TrimSpace(string(out)))
	return nodes, nil
}

// GetOpenEBSPoolsVersions returns internal IP addresses of all nodes in the cluster
func GetOpenEBSPoolsVersions(ctx context.Context) (map[string]string, error) {
	//sudo kubectl get pods --field-selector=status.phase=Running  --selector=app=cstor-pool  -nopenebs -o  jsonpath='{range .items[*]}{.metadata.labels.openebs\.io/storage-pool-claim}{" "}{.metadata.labels.openebs\.io/version}{"\n"}{end}'
	args := utils.PlanetCommand(Command("get", "pods",
		"--field-selector", "status.phase=Running",
		"--selector", "app=cstor-pool",
		"-nopenebs",
		"-o", `jsonpath={range .items[*]}{.metadata.labels.openebs\.io/storage-pool-claim}{" "}{.metadata.labels.openebs\.io/version}{"\n"}{end}`))

	return getKubectlOutput(ctx, args)
}

// GetOpenEBSPoolsVersions returns internal IP addresses of all nodes in the cluster
func GetOpenEBSVolumesVersions(ctx context.Context) (map[string]string, error) {
	// sudo kubectl get pods --field-selector=status.phase=Running  --selector=app=cstor-volume-manager,openebs\.io/storage-class=openebs-cstor  -nopenebs -o  jsonpath='{range .items[*]}{.metadata.labels.openebs\.io/persistent-volume}{" "}{.metadata.labels.openebs\.io/version}{"\n"}{end}'
	//		"--selector", `app=cstor-volume-manager,openebs.io/storage-class=openebs-cstor`,
	// !!! WORKS -> 		"--selector", `openebs.io/storage-class=openebs-cstor`,
	args := utils.PlanetCommand(Command("get", "pods",
		"--field-selector", "status.phase=Running",
		"--selector", `app=cstor-volume-manager,openebs.io/storage-class=openebs-cstor`,
		"-nopenebs",
		"-o", `jsonpath={range .items[*]}{.metadata.labels.openebs\.io/persistent-volume}{" "}{.metadata.labels.openebs\.io/version}{"\n"}{end}`))

	return getKubectlOutput(ctx, args)
}

// TODO make generic
// getKubectlOutput
// expects a kubectl command that returns one ro more lines of key values
func getKubectlOutput(ctx context.Context, args []string) (map[string]string, error) {
	spew.Dump(args)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	cmd.Stderr = utils.NewStderrLogger(log.WithField("cmd", "kubectl get pods"))

	out, err := cmd.Output()
	if err != nil {
		return nil, trace.Wrap(err, "%v : %v", cmd, err)
	}

	poolsAndVersions := strings.Split(string(out), "\n")
	spew.Dump(poolsAndVersions)

	// TODO remove duplication
	poolAndVersion := make(map[string]string)
	for _, poolAndVer := range poolsAndVersions {
		pav := strings.Split(poolAndVer, " ")
		if len(pav) != 2 {
			continue
		}

		poolAndVersion[pav[0]] = pav[1]
	}

	spew.Dump(poolAndVersion)
	return poolAndVersion, nil
}

// WithPrivilegedConfig returns a command option to specify a privileged kubeconfig
func WithPrivilegedConfig() optionSetter {
	return func(cmd *Cmd) {
		cmd.args = append(cmd.args, "--kubeconfig", constants.PrivilegedKubeconfig)
	}
}

// Command returns a new command that executes a kubectl command with optional args
func Command(args ...string) *Cmd {
	return &Cmd{command: defaults.KubectlBin, args: args}
}

// Args returns the command line for this Cmd.
// Implements utils.Command
func (r Cmd) Args() []string {
	return append([]string{r.command}, r.args...)
}

// String returns a formatted representation of this command
func (r Cmd) String() string {
	return fmt.Sprintf("%v %v", r.command, strings.Join(r.args, " "))
}

// Cmd is a kubectl command with arguments
type Cmd struct {
	command string
	args    []string
}

type optionSetter func(*Cmd)

const (
	noResourcesPrefix = "No resources found"
	tempDirPrefix     = "gravity-kubectl"
)
