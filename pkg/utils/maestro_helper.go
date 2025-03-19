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

package utils

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/openshift-online/maestro/pkg/api/openapi"
	"github.com/openshift-online/maestro/pkg/client/cloudevents/grpcsource"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetServiceURL(c client.Client, serviceName, serviceNS, protocol string) (string, error) {
	serviceKey := client.ObjectKey{
		Namespace: serviceNS,
		Name:      serviceName,
	}

	service := &corev1.Service{}
	if err := c.Get(context.TODO(), serviceKey, service); err != nil {
		return "", fmt.Errorf("failed to get service %s/%s: %v", serviceNS, serviceName, err)
	}

	if service.Spec.ClusterIP == "" {
		return "", fmt.Errorf("service %s/%s has no clusterIP", serviceNS, serviceName)
	}

	if len(service.Spec.Ports) == 0 {
		return "", fmt.Errorf("service %s/%s has no ports defined", serviceNS, serviceName)
	}

	serviceDNS := serviceName + "." + serviceNS + ".svc.cluster.local"

	// serviceURL := net.JoinHostPort(service.Spec.ClusterIP, fmt.Sprint(service.Spec.Ports[0].Port))

	serviceURL := net.JoinHostPort(serviceDNS, fmt.Sprint(service.Spec.Ports[0].Port))
	if protocol > "" {
		serviceURL = protocol + "://" + net.JoinHostPort(serviceDNS, fmt.Sprint(service.Spec.Ports[0].Port))
	}

	klog.Infof("service URL: %v", serviceURL)

	return serviceURL, nil
}

func BuildWorkClient(ctx context.Context, maestroServerAddr, grpcServerAddr string) (workv1client.WorkV1Interface, error) {
	maestroAPIClient := openapi.NewAPIClient(&openapi.Configuration{
		DefaultHeader: make(map[string]string),
		UserAgent:     "OpenAPI-Generator/1.0.0/go",
		Debug:         false,
		Servers: openapi.ServerConfigurations{
			{
				URL:         maestroServerAddr,
				Description: "current domain",
			},
		},
		OperationServers: map[string]openapi.ServerConfigurations{},
		HTTPClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			}},
			Timeout: 10 * time.Second,
		},
	})

	grpcOptions := grpc.NewGRPCOptions()
	grpcOptions.URL = grpcServerAddr

	workClient, err := grpcsource.NewMaestroGRPCSourceWorkClient(
		ctx,
		maestroAPIClient,
		grpcOptions,
		"app-work-client",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create the Maestro GRPC work client, err: %v", err)
	}

	return workClient, nil
}
