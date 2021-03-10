/**
 * Copyright (c) 2020 CoCreate LLC
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy of
 * this software and associated documentation files (the "Software"), to deal in
 * the Software without restriction, including without limitation the rights to
 * use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
 * the Software, and to permit persons to whom the Software is furnished to do so,
 * subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
 * FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
 * COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
 * IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
 * CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */

package provisioner

import (
	"fmt"

	"k8s.io/klog/v2/klogr"
)

var logger = klogr.New().WithName("provisioner")

// ProvisionerT indicates the type of provisioner
type ProvisionerT string

const (
	// ProvisionerRancherNodePool means utilizing Rancher's node pool API to provision nodes
	ProvisionerRancherNodePool ProvisionerT = "ranchernodepool"
)

// SupportProvisionerType returns the list of supporting types of provisioner
// Update this func when new type is added
func SupportProvisionerType() string {
	return fmt.Sprintf("\"%s\"", ProvisionerRancherNodePool)
}

// Provisioner is the interface for provisioning nodes
type Provisioner interface {
	Type() ProvisionerT
	// ScaleUp calls backend system to scale up ONE node.
	// Note that this func is called in an async manner,
	// and metric calculator does not rely on any response
	// from backend
	ScaleUp()
	// ScaleDown calls backend system to scale down ONE node.
	// Note that this func is called in an async manner,
	// and metric calculator does not rely on any response
	// from backend
	ScaleDown()
}
