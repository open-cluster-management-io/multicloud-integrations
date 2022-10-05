// Copyright 2019 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// AddToManagerFuncs is a list of functions to add all Controllers to the Manager
var AddToManagerFuncs []func(manager.Manager) error

var AddGitOpsClusterToManagerFuncs []func(manager.Manager) error

// AddPullModelAggregationToManagerFuncs is a list of functions to add all PullModelAggregation Controllers to the Manager
var AddPullModelAggregationToManagerFuncs []func(manager.Manager, int) error

// AddToManager adds all Controllers to the Manager
func AddToManager(m manager.Manager) error {
	for _, f := range AddToManagerFuncs {
		if err := f(m); err != nil {
			return err
		}
	}

	return nil
}

func AddGitOpsClusterToManager(m manager.Manager) error {
	for _, f := range AddGitOpsClusterToManagerFuncs {
		if err := f(m); err != nil {
			return err
		}
	}

	return nil
}

// AddPullModelAggregationToManager adds PullModelAggregation Controller to the Manager
func AddPullModelAggregationToManager(m manager.Manager, interval int) error {
	for _, f := range AddPullModelAggregationToManagerFuncs {
		if err := f(m, interval); err != nil {
			return err
		}
	}

	return nil
}
