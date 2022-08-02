package cluster

// Copyright (c) Microsoft Corporation.
// Licensed under the Apache License 2.0.

import (
	"context"
	"fmt"

	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Azure/ARO-RP/pkg/hive"
)

var clusterDeploymentConditionsExpected = map[hivev1.ClusterDeploymentConditionType]corev1.ConditionStatus{
	hivev1.ClusterReadyCondition:  corev1.ConditionTrue,
	hivev1.UnreachableCondition:   corev1.ConditionFalse,
	hivev1.SyncSetFailedCondition: corev1.ConditionFalse,
}

func (mon *Monitor) emitHiveRegistrationStatus(ctx context.Context) error {
	if mon.hiveclientset == nil {
		// TODO(hive): remove this once we have Hive everywhere
		mon.log.Info("skipping: no hive cluster manager")
		return nil
	}

	if mon.oc.Properties.HiveProfile.Namespace == "" {
		return fmt.Errorf("cluster %s not adopted. No namespace in the clusterdocument", mon.oc.Name)
	}

	return mon.validateHiveConditions(ctx)
}

func (mon *Monitor) validateHiveConditions(ctx context.Context) error {
	cd, err := mon.hiveclientset.HiveV1().ClusterDeployments(mon.oc.Properties.HiveProfile.Namespace).Get(ctx, hive.ClusterDeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	conditions := mon.filterConditions(ctx, cd, clusterDeploymentConditionsExpected)

	mon.emitMetrics(conditions)

	return nil
}

func (mon *Monitor) filterConditions(ctx context.Context, cd *hivev1.ClusterDeployment, clusterDeploymentConditionsExpected map[hivev1.ClusterDeploymentConditionType]corev1.ConditionStatus) []hivev1.ClusterDeploymentCondition {
	conditions := make([]hivev1.ClusterDeploymentCondition, 0)
	for _, condition := range cd.Status.Conditions {
		if expectedState, ok := clusterDeploymentConditionsExpected[condition.Type]; ok {
			if condition.Status != expectedState {
				conditions = append(conditions, condition)
			}
		}
	}

	return conditions
}

func (mon *Monitor) emitMetrics(conditions []hivev1.ClusterDeploymentCondition) {
	for _, condition := range conditions {
		mon.emitGauge("hive.clusterdeployment.conditions", 1, map[string]string{
			"type":   string(condition.Type),
			"reason": condition.Reason,
		})
	}
}
