/*
Copyright 2020 The Kubernetes Authors.

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

package create

import (
	"context"
	"fmt"
	"regexp"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	// TODO: Add regex explanation
	sizeRegex = `^\d+[KMGT]i?$`

	accessModeRegex = `^(ReadWriteMany|ReadWriteOncePod|ReadWriteOnce|ReadOnlyMany)$`

	pvcLong = templates.LongDesc(i18n.T(`
	Create a persitent volume claim with the specified name and size.`))

	pvcExample = templates.Examples(i18n.T(`
		# Create a persistent volume claim of size 8Gi, named example
		kubectl create pvc example --size=8Gi

		# Create a persistent volume claim, of access mode ReadWriteMany
		kubectl create pvc example --size=8Gi --accessmode=ReadWriteMany

		# Create a persistent volume claim, and specifying the storageclass
		kubectl create pvc example --size=8Gi --class=longhorn

		# Create a persistent volume claim, specifying the name of a persistent volume
		kubectl create pvc example --size=8Gi --volume=example-pv

		# Create a persistent volume claim, specifying volume mode as Filesystem
		kubectl create pvc example --size=8Gi --volumemode=filesystem
		`))
)

// CreatePVCOptions is returned by NewCmdCreatePVC
type CreatePVCOptions struct {
	PrintFlags *genericclioptions.PrintFlags

	PrintObj func(obj runtime.Object) error

	Name             string
	Size			 string
	AccessMode		 string
	StorageClass	 string
	PersistentVolume string
	VolumeMode		 string
	Namespace        string
	EnforceNamespace bool
	CreateAnnotation bool

	Client              corev1client.CoreV1Interface
	DryRunStrategy      cmdutil.DryRunStrategy
	ValidationDirective string

	FieldManager string

	genericiooptions.IOStreams
}

// NewCreatePVCOptions creates the CreatePVCOptions to be used later
func NewCreatePVCOptions(ioStreams genericiooptions.IOStreams) *CreatePVCOptions {
	return &CreatePVCOptions{
		PrintFlags: genericclioptions.NewPrintFlags("created").WithTypeSetter(scheme.Scheme),
		IOStreams:  ioStreams,
	}
}

// NewCmdCreatePVC is a macro command to create a new PVC.
// This command is better known to users as `kubectl create pvc`.
func NewCmdCreatePVC(f cmdutil.Factory, ioStreams genericiooptions.IOStreams) *cobra.Command {
	o := NewCreatePVCOptions(ioStreams)

	cmd := &cobra.Command{
		Use:                   "pvc NAME --size=8Gi ",
		DisableFlagsInUseLine: true,
		Aliases:               []string{"pvc", "persistentvolumeclaim"},
		Short:                 i18n.T("Create a pvc with the specified name and size"),
		Long:                  pvcLong,
		Example:               pvcExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run())
		},
	}

	o.PrintFlags.AddFlags(cmd)

	cmdutil.AddApplyAnnotationFlags(cmd)
	cmdutil.AddValidateFlags(cmd)
	cmdutil.AddDryRunFlag(cmd)
	cmd.Flags().StringVar(&o.Size, "size", o.Size, "Size of the persistent volume claim")
	cmd.Flags().StringVar(&o.StorageClass, "class", o.StorageClass, "Storage Class to be used")
	cmd.Flags().StringVar(&o.AccessMode, "accessMode", o.AccessMode, "AccessMode used for the PVC")
	cmd.Flags().StringVar(&o.PersistentVolume, "persistentvolume", o.PersistentVolume, "Specify a PV for the PVC to use")
	cmd.Flags().StringVar(&o.VolumeMode, "volumeMode", o.VolumeMode, "Specify the volume mode as filesystem or block")
	cmdutil.AddFieldManagerFlagVar(cmd, &o.FieldManager, "kubectl-create")

	return cmd
}

// Complete completes all the options
func (o *CreatePVCOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	name, err := NameFromCommandArgs(cmd, args)
	if err != nil {
		return err
	}
	o.Name = name

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.Client, err = corev1client.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	o.Namespace, o.EnforceNamespace, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	o.CreateAnnotation = cmdutil.GetFlagBool(cmd, cmdutil.ApplyAnnotationsFlag)

	o.DryRunStrategy, err = cmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return err
	}
	cmdutil.PrintFlagsWithDryRunStrategy(o.PrintFlags, o.DryRunStrategy)

	printer, err := o.PrintFlags.ToPrinter()
	if err != nil {
		return err
	}
	o.PrintObj = func(obj runtime.Object) error {
		return printer.PrintObj(obj, o.Out)
	}

	o.ValidationDirective, err = cmdutil.GetValidationDirective(cmd)
	return err
}

// Validate validates the Ingress object to be created
func (o *CreatePVCOptions) Validate() error {
	if len(o.Size) == 0 {
		return fmt.Errorf("not enough information provided: a size must be specified for the persistent volume claim")
	}

	sizeValidation, err := regexp.Compile(sizeRegex)
	if err != nil {
		return fmt.Errorf("failed to compile the size regex")
	}
	accessModeValidation, err := regexp.Compile(accessModeRegex)
	if err != nil {
		return fmt.Errorf("failed to compile the accessMode regex")
	}

	if !sizeValidation.MatchString(o.Size) {
		return fmt.Errorf("size %s is invalid, it should be in format integer[G/M(i)]", o.Size)
	}

	if len(o.AccessMode) > 0 && !accessModeValidation.MatchString(o.AccessMode) {
		return fmt.Errorf("accessMode %s is invalid, it should be in format RWO/ReadWriteOnce or similar", o.AccessMode)
	} else {
		// otherwise return to a sensible default
		o.AccessMode = "ReadWriteOnce"
	}

	if len(o.VolumeMode) > 0 && o.VolumeMode != "Filesystem" && o.VolumeMode != "Block" {
		return fmt.Errorf("volumeMode %s is invalid, it should be Filesystem or Block", o.AccessMode)
	} else {
		// otherwise return to a sensible default
		o.VolumeMode = "Filesystem"
	}

	return nil
}

// Run performs the execution of 'create ingress' sub command
func (o *CreatePVCOptions) Run() error {
	pvc := o.createPVC()

	if err := util.CreateOrUpdateAnnotation(o.CreateAnnotation, pvc, scheme.DefaultJSONEncoder()); err != nil {
		return err
	}

	if o.DryRunStrategy != cmdutil.DryRunClient {
		createOptions := metav1.CreateOptions{}
		if o.FieldManager != "" {
			createOptions.FieldManager = o.FieldManager
		}
		createOptions.FieldValidation = o.ValidationDirective
		if o.DryRunStrategy == cmdutil.DryRunServer {
			createOptions.DryRun = []string{metav1.DryRunAll}
		}
		var err error
		pvc, err = o.Client.PersistentVolumeClaims(o.Namespace).Create(context.TODO(), pvc, createOptions)
		if err != nil {
			return fmt.Errorf("failed to create pvc: %v", err)
		}
	}
	return o.PrintObj(pvc)
}

func (o *CreatePVCOptions) createPVC() *corev1.PersistentVolumeClaim {
	namespace := ""
	if o.EnforceNamespace {
		namespace = o.Namespace
	}

	spec := o.buildPVCSpec()

	pvc := &corev1.PersistentVolumeClaim {
		TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: "PersistentVolumeClaims"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        o.Name,
			Namespace:   namespace,
		},
		Spec: spec,
	}
	return pvc
}

// buildPVCSpec builds the .spec from the diverse arguments passed to kubectl
func (o *CreatePVCOptions) buildPVCSpec() corev1.PersistentVolumeClaimSpec {
	var pvcSpec corev1.PersistentVolumeClaimSpec

	if len(o.AccessMode) > 0 {
		pvcSpec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.PersistentVolumeAccessMode(o.AccessMode)}
	}

	if len(o.StorageClass) > 0 {
		pvcSpec.StorageClassName = &o.StorageClass
	}

	volumeMode := corev1.PersistentVolumeMode(o.VolumeMode)
	pvcSpec.VolumeMode = &volumeMode

	pvcSpec.VolumeName = *&o.Name
	pvcSpec.Resources = corev1.VolumeResourceRequirements{
	    Requests: corev1.ResourceList{
	        corev1.ResourceStorage: resource.MustParse(o.Size),
	    },
	}

	return pvcSpec
}
