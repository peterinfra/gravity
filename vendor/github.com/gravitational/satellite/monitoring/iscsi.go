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

package monitoring

import (
	"context"
	"strings"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/gravitational/satellite/agent/health"
	pb "github.com/gravitational/satellite/agent/proto/agentpb"
	"github.com/gravitational/trace"
)

const (
	iscsiCheckerID = "iscsi"
)

type FormatISCSIError func(unitName string) string

// NewISCSIChecker returns a new checker, that checks that the iscsid is not running on the host when
// OpenEBS is enabled in the deployment manifest. This is needed because if iscsid is running on the host it
// makes the iscsid in planet to fail.
func NewISCSIChecker(fmt FormatISCSIError) health.Checker {
	return &iscsiChecker{FailedProbeMsgFmt: fmt}
}

type iscsiChecker struct{
	FailedProbeMsgFmt FormatISCSIError
}

// Name returns the name of this checker
// Implements health.Checker
func (c iscsiChecker) Name() string {
	return iscsiCheckerID
}

// Check will check the systemd unit data coming from dbus to verify that
// there are no iscsid services present on the host.
func (c iscsiChecker) Check(ctx context.Context, reporter health.Reporter) {
	conn, err := dbus.New()
	if err != nil {
		reason := "failed to connect to dbus"
		reporter.Add(NewProbeFromErr(c.Name(), reason, trace.Wrap(err)))
		return
	}
	defer conn.Close()

	units, err := conn.ListUnits()
	if err != nil {
		reason := "failed to query systemd units"
		reporter.Add(NewProbeFromErr(c.Name(), reason, trace.Wrap(err)))
		return
	}
	
	for _, unit := range units {
		if strings.Contains(unit.Name, "iscsid.service") || strings.Contains(unit.Name, "iscsid.socket") {
			if unit.LoadState != loadStateMasked || unit.ActiveState == "active" {
				reporter.Add(&pb.Probe{
					Checker: iscsiCheckerID,
					Detail: c.FailedProbeMsgFmt(unit.Name),
					Status: pb.Probe_Failed,
				})
			}
		}
	}

	if reporter.NumProbes() == 0 {
		reporter.Add(NewSuccessProbe(c.Name()))
	}
}