package cloudresources

import (
	"context"
	"fmt"
	cloudresourcesv1beta1 "github.com/kyma-project/cloud-manager/api/cloud-resources/v1beta1"
	"github.com/kyma-project/cloud-manager/pkg/composed"
	"github.com/kyma-project/cloud-manager/pkg/util"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func checkIfResourcesExist(ctx context.Context, st composed.State) (error, context.Context) {
	state := st.(*State)
	logger := composed.LoggerFromCtx(ctx)

	var foundKinds []string

	for gvk := range state.Cluster().Scheme().AllKnownTypes() {
		if gvk.Group != cloudresourcesv1beta1.GroupVersion.Group {
			continue
		}
		if gvk.Kind == "CloudResources" {
			continue
		}
		if strings.HasSuffix(gvk.Kind, "List") {
			continue
		}

		listGvk := schema.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind + "List",
		}
		if !state.Cluster().Scheme().Recognizes(listGvk) {
			continue
		}
		listObj, err := state.Cluster().Scheme().New(listGvk)
		if runtime.IsNotRegisteredError(err) {
			continue
		}
		if err != nil {
			logger.
				WithValues(
					"errorType", fmt.Errorf("%T", err),
					"gvk", listGvk.String(),
				).
				Error(err, "Error instantiating GVK list object")
			continue
		}
		list := listObj.(client.ObjectList)

		err = state.Cluster().K8sClient().List(ctx, list)
		if meta.IsNoMatchError(err) {
			// this CRD is not installed
			continue
		}
		if err != nil {
			logger.
				WithValues(
					"errorType", fmt.Sprintf("%T", err),
					"gvk", gvk.String(),
					"listGvk", listGvk.String(),
				).
				Error(err, "Error listing GVK")
			continue
		}

		if meta.LenList(list) == 0 {
			continue
		}

		foundKinds = append(foundKinds, gvk.Kind)
	}

	if len(foundKinds) == 0 {
		return nil, nil
	}

	logger.
		WithValues("existingResourceKinds", foundKinds).
		Info("Can not deactivate module due to found resources")

	state.ObjAsCloudResources().Status.State = cloudresourcesv1beta1.ModuleState(util.KymaModuleStateWarning)

	return composed.UpdateStatus(state.ObjAsCloudResources()).
		RemoveConditions(cloudresourcesv1beta1.ConditionTypeReady).
		SetExclusiveConditions(metav1.Condition{
			Type:    cloudresourcesv1beta1.ConditionTypeWarning,
			Status:  metav1.ConditionTrue,
			Reason:  cloudresourcesv1beta1.ConditionReasonResourcesExist,
			Message: fmt.Sprintf("Can not deactivate module while cloud resources exist: %v", foundKinds),
		}).
		SuccessError(composed.StopWithRequeueDelay(util.Timing.T10000ms())).
		Run(ctx, state)
}
