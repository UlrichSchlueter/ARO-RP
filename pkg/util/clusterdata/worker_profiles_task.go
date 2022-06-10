package clusterdata

// Copyright (c) Microsoft Corporation.
// Licensed under the Apache License 2.0.

import (
	"context"
	"fmt"
	"sort"

	"github.com/Azure/go-autorest/autorest/azure"
	machineclient "github.com/openshift/client-go/machine/clientset/versioned"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"github.com/Azure/ARO-RP/pkg/api"
	utilmachine "github.com/Azure/ARO-RP/pkg/util/machine"
	_ "github.com/Azure/ARO-RP/pkg/util/scheme"
)

const (
	workerMachineSetsNamespace = "openshift-machine-api"
)

func newWorkerProfilesEnricherTask(log *logrus.Entry, restConfig *rest.Config, oc *api.OpenShiftCluster) (enricherTask, error) {
	maocli, err := machineclient.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	return &workerProfilesEnricherTask{
		log:    log,
		maocli: maocli,
		oc:     oc,
	}, nil
}

type workerProfilesEnricherTask struct {
	log    *logrus.Entry
	maocli machineclient.Interface
	oc     *api.OpenShiftCluster
}

func (ef *workerProfilesEnricherTask) FetchData(ctx context.Context, callbacks chan<- func(), errs chan<- error) {
	r, err := azure.ParseResourceID(ef.oc.ID)
	if err != nil {
		ef.log.Error(err)
		errs <- err
		return
	}

	machinesets, err := ef.maocli.MachineV1beta1().MachineSets(workerMachineSetsNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		ef.log.Error(err)
		errs <- err
		return
	}

	workerProfiles := make([]api.WorkerProfile, len(machinesets.Items))
	for i, machineset := range machinesets.Items {
		workerCount := 1
		if machineset.Spec.Replicas != nil {
			workerCount = int(*machineset.Spec.Replicas)
		}

		workerProfiles[i] = api.WorkerProfile{
			Name:  machineset.Name,
			Count: workerCount,
		}

		if machineset.Spec.Template.Spec.ProviderSpec.Value == nil {
			ef.log.Infof("provider spec is missing in the machine set %q", machineset.Name)
			continue
		}

		machineProviderSpec, err := utilmachine.UnmarshalAzureProviderSpec(machineset.Name, utilmachine.MachineSet, machineset.Spec.Template.Spec.ProviderSpec.Value.Raw)

		if err != nil {
			// If this happens, the azure machine provider spec type/apiversion may have been updated and
			// we need to handle it appropriately
			ef.log.Info(err.Error())
			continue
		}

		workerProfiles[i].VMSize = api.VMSize(machineProviderSpec.VMSize)
		workerProfiles[i].DiskSizeGB = int(machineProviderSpec.OSDisk.DiskSizeGB)
		workerProfiles[i].SubnetID = fmt.Sprintf(
			"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s/subnets/%s",
			r.SubscriptionID, machineProviderSpec.NetworkResourceGroup, machineProviderSpec.Vnet, machineProviderSpec.Subnet,
		)

		encryptionAtHost := api.EncryptionAtHostDisabled
		if machineProviderSpec.SecurityProfile != nil &&
			machineProviderSpec.SecurityProfile.EncryptionAtHost != nil &&
			*machineProviderSpec.SecurityProfile.EncryptionAtHost {
			encryptionAtHost = api.EncryptionAtHostEnabled
		}

		workerProfiles[i].EncryptionAtHost = encryptionAtHost

		if machineProviderSpec.OSDisk.ManagedDisk.DiskEncryptionSet != nil {
			workerProfiles[i].DiskEncryptionSetID = machineProviderSpec.OSDisk.ManagedDisk.DiskEncryptionSet.ID
		}
	}

	sort.Slice(workerProfiles, func(i, j int) bool { return workerProfiles[i].Name < workerProfiles[j].Name })

	callbacks <- func() {
		ef.oc.Properties.WorkerProfiles = workerProfiles
	}
}

func (ef *workerProfilesEnricherTask) SetDefaults() {
	ef.oc.Properties.WorkerProfiles = nil
}
