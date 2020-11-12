/*
Copyright 2020 Gravitational, Inc.

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

package phases

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gravitational/gravity/lib/app/hooks"
	"github.com/gravitational/gravity/lib/storage"
	"os/exec"
	"strings"
	"text/template"

	"github.com/gravitational/gravity/lib/fsm"
	"github.com/gravitational/gravity/lib/utils"

	"github.com/gravitational/trace"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"k8s.io/client-go/kubernetes"
)

// Upgrade ETCD
// Upgrading etcd to etcd 3 is a somewhat complicated process.
// According to the etcd documentation, upgrades of a cluster are only supported one release at a time. Since we are
// several versions behind, coordinate several upgrades in succession has a certain amount of risk and may also be
// time consuming.
//
// The chosen approach to upgrades of etcd is as follows
// 1. Planet will ship with each version of etcd we support upgrades from
// 2. Planet when started, will determine the version of etcd to use (planet etcd init)
//      This is done by assuming the oldest possible etcd release
//      During an upgrade, the verison of etcd to use is written to the etcd data directory
// 3. Backup all etcd data via API
// 4. Shutdown etcd (all servers) // API outage starts
// 6. Start the cluster masters, but with clients bound to an alternative address (127.0.0.2) and using new data dir
//      The data directory is chosen as /ext/etcd/<version>, so when upgrading, etcd will start with a blank database
//      To rollback, we start the old version of etcd, pointed to the data directory from the previous version
//      We also delete the data from a previous upgrade, so we can only roll back once
// 7. Shutdown the temporary etcd node, and do an offline database restore to the newly created database
// 8. Restore the etcd data using the API to the new version, and migrate /registry (kubernetes) data to v3 datastore
// 9. Restart etcd on the correct ports// API outage ends
// 10. Restart gravity-site to fix elections
//
//
// Rollback
// Stop etcd (all servers)
// Set the version to use to be the previous version
// Restart etcd using the old version, pointed at the old data directory
// Start etcd (all servers)

// PhaseUpgradePool backs up etcd data on all servers
type PhaseUpgradePool struct {
	// FieldLogger is used for logging
	log.FieldLogger
	// Client is an API client to the kubernetes API
	Client      *kubernetes.Clientset
	Pool        string
	PoolVersion string
}

func NewPhaseUpgradePool(phase storage.OperationPhase, client *kubernetes.Clientset, logger log.FieldLogger) (fsm.PhaseExecutor, error) {

	volAndVer := strings.Split(phase.Data.Data, " ")
	return &PhaseUpgradePool{
		FieldLogger: logger,
		Client:      client,
		Pool:        volAndVer[0],
		PoolVersion: volAndVer[1],
	}, nil
}

func (p *PhaseUpgradePool) Execute(ctx context.Context) error {
	p.Info("Upgrading OpenEBS poolsAndVersion.")

	err := p.executeUpgradeCmd(ctx, p.Pool, p.PoolVersion)
	if err != nil {
		return trace.Wrap(err)
	}

	return nil
}

type PoolUpgrade struct {
	FromVersion string
	Pool        string
}

func (p *PhaseUpgradePool) executeUpgradeCmd(ctx context.Context, pool string, version string) error {
	var buf bytes.Buffer
	err := poolUpgradeTemplate.Execute(&buf, &PoolUpgrade{FromVersion: version, Pool: pool})
	if err != nil {
		return trace.Wrap(err)
	}

	p.Infof("Got rendered template %v:", buf.String())

	err = ioutil.WriteFile("cstor_pool_upgrade.yaml", buf.Bytes(), 0644)
	if err != nil {
		return trace.Wrap(err)
	}

	var out bytes.Buffer
	if err := utils.Exec(exec.Command("/bin/bash", "-c", "ls -lath && kubectl apply -f cstor_pool_upgrade.yaml"), &out); err != nil {
		p.Warnf("Failed exec command. Got output %v:", out.String())
		return trace.Wrap(err)
	}

	p.Infof("Got output %v:", out.String())

	runner, err := hooks.NewRunner(p.Client)
	if err != nil {
		return trace.Wrap(err)
	}

	p.Infof("streaming job output :1")
	fmt.Printf("streaming job output 2:")
	// TODO parametrize job name with the template
	upgradeJobLog := utils.NewSyncBuffer()
	err = runner.StreamLogs(ctx, hooks.JobRef{Name: "cstor-spc-1170220", Namespace: "openebs"}, upgradeJobLog)
	if err != nil {
		p.Warnf("Failed to stream logs.")
		return trace.Wrap(err)
	}
	p.Infof(" got logs: %v", upgradeJobLog.String())
	p.Infof("streaming job output :3")
	fmt.Printf("streaming job output 4:")
	return nil
}

func (p *PhaseUpgradePool) Rollback(context.Context) error {
	// NOOP, don't clean up backupfile during rollback, incase we still need it
	return nil
}

func (*PhaseUpgradePool) PreCheck(ctx context.Context) error {
	// TODO(lenko) Check the version of the existing volumes
	return nil
}

func (*PhaseUpgradePool) PostCheck(context.Context) error {
	// TODO(lenko) Check the version of the new volumes
	return nil
}

var poolUpgradeTemplate = template.Must(template.New("upgradePool").Parse(`
#This is an example YAML for upgrading cstor SPC.
#Some of the values below needs to be changed to
#match your openebs installation. The fields are
#indicated with VERIFY
---
apiVersion: batch/v1
kind: Job
metadata:
  #VERIFY that you have provided a unique name for this upgrade job.
  #The name can be any valid K8s string for name. This example uses
  #the following convention: cstor-spc-<flattened-from-to-versions>
  name: cstor-spc-1170220

  #VERIFY the value of namespace is same as the namespace where openebs components
  # are installed. You can verify using the command:
  # kubectl get pods -n <openebs-namespace> -l openebs.io/component-name=maya-apiserver
  # The above command should return status of the openebs-apiserver.
  namespace: openebs
spec:
  template:
    spec:
      #VERIFY the value of serviceAccountName is pointing to service account
      # created within openebs namespace. Use the non-default account.
      # by running kubectl get sa -n <openebs-namespace>
      serviceAccountName: openebs-maya-operator
      containers:
      - name:  upgrade
        args:
        - "cstor-spc"

        # --from-version is the current version of the pool
        - "--from-version={{.FromVersion}}"

        # --to-version is the version desired upgrade version
        - "--to-version=2.2.0"

        # Bulk upgrade is supported
        # To make use of it, please provide the list of SPCs
        # as mentioned below
        - "{{.Pool}}"

        #Following are optional parameters
        #Log Level
        - "--v=4"
        #DO NOT CHANGE BELOW PARAMETERS
        env:
        - name: OPENEBS_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        tty: true

        # the image version should be same as the --to-version mentioned above
        # in the args of the job
        image: openebs/m-upgrade:2.2.0
        imagePullPolicy: Always
      restartPolicy: Never
---
`))

// PhaseUpgradeVolumes backs up etcd data on all servers
type PhaseUpgradeVolumes struct {
	// FieldLogger is used for logging
	log.FieldLogger
	// Client is an API client to the kubernetes API
	Client        *kubernetes.Clientset
	Volume        string
	VolumeVersion string
}

func NewPhaseUpgradeVolumes(phase storage.OperationPhase, client *kubernetes.Clientset, logger log.FieldLogger) (fsm.PhaseExecutor, error) {

	volAndVer := strings.Split(phase.Data.Data, " ")
	return &PhaseUpgradeVolumes{
		FieldLogger:   logger,
		Client:        client,
		Volume:        volAndVer[0],
		VolumeVersion: volAndVer[1],
	}, nil
}

func (p *PhaseUpgradeVolumes) Execute(ctx context.Context) error {
	p.Info("Upgrading OpenEBS volumesAndVersion.")

	// kubectl get pv -A | grep openebs-cstor | cut -d' ' -f1 | grep pvc
	//var out bytes.Buffer
	//	if err := utils.Exec(exec.Command("kubectl", "get", "pv", "-A", "|", "grep", "openebs-cstor","|","cut","-d' '","-f1","|","grep","pvc"), &out); err != nil {
	//	if err := utils.Exec(exec.Command("/bin/bash", "-c", "ls -lath | grep 'drw'  | cut -d' ' -f1 | grep 'drw'"), &out); err != nil {
	//	if err := utils.Exec(exec.Command("/bin/bash", "-c", "ls -lath | grep 'drw'  | cut -d' ' -f1 | grep 'drw'"), &out); err != nil {
	// TODO use kubectl.Command("get","pods","--field-selector","status.phase=Running","--selector=app","cstor-volAndVer-manager,openebs\.io/storage-class=openebs-cstor","-n","openebs","-o","jsonpath='{.items[*].metadata.labels.openebs\.io/persistent-volAndVer}{" "}{.items[*].metadata.labels.openebs\.io/version}'")
	/*if err := utils.Exec(exec.Command("/bin/bash", "-c", "kubectl get pods --field-selector=status.phase=Running  --selector=app=cstor-volAndVer-manager,openebs\\.io/storage-class=openebs-cstor  -nopenebs -o  jsonpath='{.items[*].metadata.labels.openebs\\.io/persistent-volAndVer}{\" \"}{.items[*].metadata.labels.openebs\\.io/version}'"), &out); err != nil {
		p.Warnf("Failed exec command. Got output %v:", out.String())
		return trace.Wrap(err)
	}*/

	//p.Infof("Got volumesAndVersion %v:", out.String())
	//commandOutput := "pvc-b6cb4c20-6e5b-42c4-8884-7c16d0e052aa\npvc-b6cb4c20-6e5b-42c4-8884-7c16d0e052ZZ"
	//commandOutput := "pvc-b363b688-8697-4628-b744-6d943e0b8ed1 1.7.0\npvc-b363b688-8697-4628-b744-6d943e0b8ZZZ 1.7.0"

	err := p.executeVolumeUpgradeCmd(ctx, p.Volume, p.VolumeVersion)
	if err != nil {
		return trace.Wrap(err)
	}

	return nil
}

type VolumeUpgrade struct {
	FromVersion string
	Volume      string
}

func (p *PhaseUpgradeVolumes) executeVolumeUpgradeCmd(ctx context.Context, volume string, version string) error {
	var buf bytes.Buffer
	err := volumeUpgradeTemplate.Execute(&buf, &VolumeUpgrade{FromVersion: version, Volume: volume})
	if err != nil {
		return trace.Wrap(err)
	}

	p.Infof("Got rendered template %v:", buf.String())

	err = ioutil.WriteFile("cstor_volume_upgrade.yaml", buf.Bytes(), 0644)
	if err != nil {
		return trace.Wrap(err)
	}

	var out bytes.Buffer
	if err := utils.Exec(exec.Command("/bin/bash", "-c", "kubectl apply -f cstor_volume_upgrade.yaml"), &out); err != nil {
		p.Warnf("Failed exec command. Got output %v:", out.String())
		return trace.Wrap(err)
	}

	runner, err := hooks.NewRunner(p.Client)
	if err != nil {
		return trace.Wrap(err)
	}

	p.Infof("streaming:1")
	fmt.Printf("streaming 2:")
	upgradeJobLog := utils.NewSyncBuffer()
	//  TODO paremtrize job name with the value in template
	err = runner.StreamLogs(ctx, hooks.JobRef{Name: "cstor-vol-170220", Namespace: "openebs"}, upgradeJobLog)
	if err != nil {
		return trace.Wrap(err)
	}

	p.Infof(" got logs: %v", upgradeJobLog.String())
	p.Infof("streaming:2")
	fmt.Printf("streaming 3:")
	return nil
}

var volumeUpgradeTemplate = template.Must(template.New("upgradeVolumes").Parse(`
#This is an example YAML for upgrading cstor volume.
#Some of the values below needs to be changed to
#match your openebs installation. The fields are
#indicated with VERIFY
---
apiVersion: batch/v1
kind: Job
metadata:
  #VERIFY that you have provided a unique name for this upgrade job.
  #The name can be any valid K8s string for name. This example uses
  #the following convention: cstor-vol-<flattened-from-to-versions>
  name: cstor-vol-170220

  #VERIFY the value of namespace is same as the namespace
  # where openebs components
  # are installed. You can verify using the command:
  # kubectl get pods -n <openebs-namespace> -l
  # openebs.io/component-name=maya-apiserver
  # The above command should return status of the openebs-apiserver.
  namespace: openebs


spec:
  template:
    spec:
      #VERIFY the value of serviceAccountName is pointing to service account
      # created within openebs namespace. Use the non-default account.
      # by running kubectl get sa -n <openebs-namespace>
      serviceAccountName: openebs-maya-operator
      containers:
        - name: upgrade
          args:
            - "cstor-volume"

            # --from-version is the current version of the volume
            - "--from-version={{.FromVersion}}"

            # --to-version is the version desired upgrade version
            - "--to-version=2.2.0"

            # Bulk upgrade is supported from 1.9
            # To make use of it, please provide the list of PVs
            # as mentioned below
            - "{{.Volume}}"

            #Following are optional parameters
            #Log Level
            - "--v=4"
          #DO NOT CHANGE BELOW PARAMETERS
          env:
            - name: OPENEBS_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
          tty: true

          # the image version should be same as the --to-version mentioned above
          # in the args of the job
          image: quay.io/openebs/m-upgrade:2.2.0
          imagePullPolicy: Always
      restartPolicy: Never
---
`))

func (p *PhaseUpgradeVolumes) Rollback(context.Context) error {
	// NOOP, don't clean up backupfile during rollback, incase we still need it
	return nil
}

func (*PhaseUpgradeVolumes) PreCheck(ctx context.Context) error {
	// TODO(lenko) Check the version of the existing volumes
	return nil
}

func (*PhaseUpgradeVolumes) PostCheck(context.Context) error {
	// TODO(lenko) Check the version of the new volumes
	return nil
}
