/*
Copyright 2017 Gravitational, Inc.

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
	"fmt"
	"strings"

	"github.com/gravitational/satellite/agent/health"
	pb "github.com/gravitational/satellite/agent/proto/agentpb"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/gravitational/trace"
)

// NewAWSHasProfileChecker returns a new checker, that checks that the instance
// has a node profile assigned to it.
// TODO(knisbet): look into enhancing this to check the contents of the profile
// for missing permissions. However, for now this exists just as a basic check
// for instances that accidently lose their profile assignment.
func NewISCSIChecker() health.Checker {
	return &iscsiChecker{}
}

type iscsiChecker struct{}

// Name returns this checker name
// Implements health.Checker
func (c iscsiChecker) Name() string {
	return awsHasProfileCheckerID
}

// Check will check the metadata API to see if an IAM profile is assigned to the node
// Implements health.Checker
func (c iscsiChecker) Check(ctx context.Context, reporter health.Reporter) {
	conn, err := dbus.New()
	if err != nil {
		reason := "failed to connect to dbus"
		reporter.Add(NewProbeFromErr(c.Name(), reason, trace.Wrap(err)))
	}
	defer conn.Close()

	var units []dbus.UnitStatus
	units, err = conn.ListUnits()
	if err != nil {
		reason := "failed to query systemd units"
		reporter.Add(NewProbeFromErr(c.Name(), reason, trace.Wrap(err)))
	}
	
	for _, unit := range units {
		fmt.Println("2TEMP unit.Name=%v", unit.Name)
		if strings.Contains(unit.Name, "iscsi") {
			reporter.Add(&pb.Probe{
				Checker: iscsiCheckerID,
				Detail:  fmt.Sprintf("iscsi conflicting program running: %v", unit.Name ),
				Status:  pb.Probe_Failed,
			})
		}
	}
	
/*
	session, err := session.NewSession()
	if err != nil {
		reporter.Add(NewProbeFromErr(awsHasProfileCheckerID, "failed to create session", trace.Wrap(err)))
		return
	}
	metadata := ec2metadata.New(session)

	_, err = metadata.IAMInfo()
	if err != nil {
		reporter.Add(NewProbeFromErr(awsHasProfileCheckerID, "failed to determine node IAM profile", trace.Wrap(err)))
		return
	}
	reporter.Add(NewSuccessProbe(awsHasProfileCheckerID))

 */
}



const (
	iscsiCheckerID = "iscsi"
)
