//
// autoscaling: This package provides types and functions to interact with the AWS Auto Scale API
//
// Depends on https://wiki.ubuntu.com/goamz
//
// Written by Boyan Dimitrov <boyan.dimitrov@hailocab.com>
// Maintained by the Hailo Platform Team <platform@hailocab.com>

package autoscaling

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"github.com/hailocab/goamz/aws"
	"log"
	"net/http"
	"net/http/httputil"
	"sort"
	"strconv"
	"strings"
	"time"
)

const debug = false

// The AutoScaling type encapsulates operations within a specific EC2 region.
type AutoScaling struct {
	aws.Auth
	aws.Region
	private byte // Reserve the right of using private data.
}

// New creates a new AutoScaling Client.
func New(auth aws.Auth, region aws.Region) *AutoScaling {
	return &AutoScaling{auth, region, 0}
}

// ----------------------------------------------------------------------------
// Filtering helper.

// Filter builds filtering parameters to be used in an autoscaling query which supports
// filtering.  For example:
//
//     filter := NewFilter()
//     filter.Add("architecture", "i386")
//     filter.Add("launch-index", "0")
//     resp, err := as.DescribeTags(filter,nil,nil)
//
type Filter struct {
	m map[string][]string
}

// NewFilter creates a new Filter.
func NewFilter() *Filter {
	return &Filter{make(map[string][]string)}
}

// Add appends a filtering parameter with the given name and value(s).
func (f *Filter) Add(name string, value ...string) {
	f.m[name] = append(f.m[name], value...)
}

func (f *Filter) addParams(params map[string]string) {
	if f != nil {
		a := make([]string, len(f.m))
		i := 0
		for k := range f.m {
			a[i] = k
			i++
		}
		sort.StringSlice(a).Sort()
		for i, k := range a {
			prefix := "Filters.member." + strconv.Itoa(i+1)
			params[prefix+".Name"] = k
			for j, v := range f.m[k] {
				params[prefix+".Values.member."+strconv.Itoa(j+1)] = v
			}
		}
	}
}

// ----------------------------------------------------------------------------
// Request dispatching logic.

// Error encapsulates an error returned by the AWS Auto Scaling API.
//
// See http://goo.gl/VZGuC for more details.
type Error struct {
	// HTTP status code (200, 403, ...)
	StatusCode int
	// AutoScaling error code ("ResourceInUse", ...)
	Code string
	// The human-oriented error message
	Message   string
	RequestId string `xml:"RequestID"`
}

func (err *Error) Error() string {
	if err.Code == "" {
		return err.Message
	}

	return fmt.Sprintf("%s (%s)", err.Message, err.Code)
}

type xmlErrors struct {
	RequestId string  `xml:"RequestId"`
	Errors    []Error `xml:"Error"`
}

func (as *AutoScaling) query(params map[string]string, resp interface{}) error {
	params["Version"] = "2011-01-01"

	data := strings.NewReader(prepareParams(params))

	hreq, err := http.NewRequest("POST", as.Region.AutoScalingEndpoint+"/", data)
	if err != nil {
		return err
	}

	hreq.Header.Set("Content-Type", "application/x-www-form-urlencoded; param=value")

	token := as.Auth.Token()
	if token != "" {
		hreq.Header.Set("X-Amz-Security-Token", token)
	}

	signer := aws.NewV4Signer(as.Auth, "autoscaling", as.Region)
	signer.Sign(hreq)

	if debug {
		log.Printf("%v -> {\n", hreq)
	}
	r, err := http.DefaultClient.Do(hreq)

	if err != nil {
		log.Printf("Error calling Amazon %v", err)
		return err
	}

	defer r.Body.Close()

	if debug {
		dump, _ := httputil.DumpResponse(r, true)
		log.Printf("response:\n")
		log.Printf("%v\n}\n", string(dump))
	}
	if r.StatusCode != 200 {
		return buildError(r)
	}
	err = xml.NewDecoder(r.Body).Decode(resp)
	return err
}

func buildError(r *http.Response) error {
	var (
		err    Error
		errors xmlErrors
	)
	xml.NewDecoder(r.Body).Decode(&errors)
	if len(errors.Errors) > 0 {
		err = errors.Errors[0]
	}

	err.RequestId = errors.RequestId
	err.StatusCode = r.StatusCode
	if err.Message == "" {
		err.Message = r.Status
	}
	return &err
}

func makeParams(action string) map[string]string {
	params := make(map[string]string)
	params["Action"] = action
	return params
}

func prepareParams(params map[string]string) string {
	var keys, sarray []string

	for k, _ := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sarray = append(sarray, aws.Encode(k)+"="+aws.Encode(params[k]))
	}

	return strings.Join(sarray, "&")
}

func addParamsList(params map[string]string, label string, ids []string) {
	for i, id := range ids {
		params[label+"."+strconv.Itoa(i+1)] = id
	}
}

// ----------------------------------------------------------------------------
// Autoscaling group management functions and types.

// EnabledMetric encapsulates a metric associated with an Auto Scaling Group
//
// See http://goo.gl/hXiH17 for more details
type EnabledMetric struct {
	Granularity string // The granularity of the enabled metric.
	Metric      string // The name of the enabled metric.
}

// Instance encapsulates an instance type as returned by the Auto Scaling API
//
// See http://goo.gl/NwBxGh and http://goo.gl/OuoqhS for more details.
type Instance struct {
	// General instance information
	AutoScalingGroupName    string
	AvailabilityZone        string
	HealthStatus            string
	InstanceId              string
	LaunchConfigurationName string
	LifecycleState          string // Can be one of Pending | Quarantined | InService | Terminating | Terminated
}

// SuspenedProcess encapsulates an Auto Scaling process that has been suspended
//
// See http://goo.gl/iObPgF for more details
type SuspendedProcess struct {
	ProcessName      string
	SuspensionReason string
}

//Tag encapsulates tag applied to an Auto Scaling group.
//
// See http://goo.gl/MG1hqs for more details
type Tag struct {
	Key               string
	PropagateAtLaunch bool   // Specifies whether the new tag will be applied to instances launched after the tag is created
	ResourceId        string // the name of the Auto Scaling group - not required if creating ASG
	ResourceType      string // currently only auto-scaling-group is supported - not required if creating ASG
	Value             string
}

// AutoScalingGroup encapsulates an Auto Scaling Group object
//
// See http://goo.gl/fJdYhg for more details.
type AutoScalingGroup struct {
	AutoScalingGroupARN     string
	AutoScalingGroupName    string
	AvailabilityZones       []string `xml:"AvailabilityZones>member"`
	CreatedTime             time.Time
	DefaultCooldown         int
	DesiredCapacity         int
	EnabledMetrics          []EnabledMetric `xml:"EnabledMetric>member"`
	HealthCheckGracePeriod  int
	HealthCheckType         string
	Instances               []Instance `xml:"Instances>member"`
	LaunchConfigurationName string
	LoadBalancerNames       []string `xml:"LoadBalancerNames>member"`
	MaxSize                 int
	MinSize                 int
	PlacementGroup          string
	Status                  string
	SuspendedProcesses      []SuspendedProcess `xml:"SuspendedProcesses>member"`
	Tags                    []Tag              `xml:"Tags>member"`
	TerminationPolicies     []string           `xml:"TerminationPolicies>member"`
	VPCZoneIdentifier       string
}

// The CreateAutoScalingGroup type encapsulates options for the respective request.
//
// See http://goo.gl/3S13Bv for more details.
type CreateAutoScalingGroup struct {
	AutoScalingGroupName    string
	AvailabilityZones       []string
	DefaultCooldown         int
	DesiredCapacity         int
	HealthCheckGracePeriod  int
	HealthCheckType         string
	InstanceId              string
	LaunchConfigurationName string
	LoadBalancerNames       []string
	MaxSize                 int
	MinSize                 int
	PlacementGroup          string
	Tags                    []Tag
	TerminationPolicies     []string
	VPCZoneIdentifier       string
}

// Generic response type containing only requiest id
type GenericResp struct {
	RequestId string `xml:"ResponseMetadata>RequestId"`
}

// Attach running instances to an autoscaling group
//
// See http://goo.gl/zDZbuQ for more details.
func (as *AutoScaling) AttachInstances(name string, instanceIds []string) (resp *GenericResp, err error) {
	params := makeParams("AttachInstances")
	params["AutoScalingGroupName"] = name

	for i, id := range instanceIds {
		key := fmt.Sprintf("InstanceIds.member.%d", i+1)
		params[key] = id
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Creates an Auto Scaling Group on AWS
//
// Required params: AutoScalingGroupName, MinSize, MaxSize
//
// See http://goo.gl/3S13Bv for more details.
func (as *AutoScaling) CreateAutoScalingGroup(options *CreateAutoScalingGroup) (resp *GenericResp, err error) {
	params := makeParams("CreateAutoScalingGroup")

	params["AutoScalingGroupName"] = options.AutoScalingGroupName
	params["MaxSize"] = strconv.Itoa(options.MaxSize)
	params["MinSize"] = strconv.Itoa(options.MinSize)
	params["DesiredCapacity"] = strconv.Itoa(options.DesiredCapacity)

	if options.DefaultCooldown != 0 {
		params["DefaultCooldown"] = strconv.Itoa(options.DefaultCooldown)
	}

	if options.HealthCheckGracePeriod != 0 {
		params["HealthCheckGracePeriod"] = strconv.Itoa(options.HealthCheckGracePeriod)
	}

	if options.HealthCheckType != "" {
		params["HealthCheckType"] = options.HealthCheckType
	}

	if options.InstanceId != "" {
		params["InstanceId"] = options.InstanceId
	}

	if options.LaunchConfigurationName != "" {
		params["LaunchConfigurationName"] = options.LaunchConfigurationName
	}

	if options.PlacementGroup != "" {
		params["PlacementGroup"] = options.PlacementGroup
	}

	if options.VPCZoneIdentifier != "" {
		params["VPCZoneIdentifier"] = options.VPCZoneIdentifier
	}

	for i, lb := range options.LoadBalancerNames {
		key := fmt.Sprintf("LoadBalancerNames.member.%d", i+1)
		params[key] = lb
	}

	for i, az := range options.AvailabilityZones {
		key := fmt.Sprintf("AvailabilityZones.member.%d", i+1)
		params[key] = az
	}

	for i, t := range options.Tags {
		key := "Tags.member.%d.%s"
		index := i + 1
		params[fmt.Sprintf(key, index, "Key")] = t.Key
		params[fmt.Sprintf(key, index, "Value")] = t.Value
		params[fmt.Sprintf(key, index, "PropagateAtLaunch")] = strconv.FormatBool(t.PropagateAtLaunch)
	}

	for i, tp := range options.TerminationPolicies {
		key := fmt.Sprintf("TerminationPolicies.member.%d", i+1)
		params[key] = tp
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// EBS represents the AWS EBS volume data type
//
// See http://goo.gl/nDUL2h for more details
type EBS struct {
	DeleteOnTermination bool
	Iops                int
	SnapshotId          string
	VolumeSize          int
	VolumeType          string
}

// BlockDeviceMapping represents the association of a block device with ebs volume.
//
// See http://goo.gl/wEGwkU for more details.
type BlockDeviceMapping struct {
	DeviceName  string
	Ebs         EBS
	NoDevice    bool
	VirtualName string
}

// InstanceMonitoring data type
//
// See http://goo.gl/TfaPwz for more details
type InstanceMonitoring struct {
	Enabled bool
}

// The CreateLaunchConfiguration type encapsulates options for the respective request.
//
// See http://goo.gl/Uw916w for more details.
type CreateLaunchConfiguration struct {
	AssociatePublicIpAddress bool
	BlockDeviceMappings      []BlockDeviceMapping
	EbsOptimized             bool
	IamInstanceProfile       string
	ImageId                  string
	InstanceId               string
	InstanceMonitoring       InstanceMonitoring
	InstanceType             string
	KernelId                 string
	KeyName                  string
	LaunchConfigurationName  string
	RamdiskId                string
	SecurityGroups           []string
	SpotPrice                string
	UserData                 string
}

// Creates an Auto Scaling Group on AWS
//
// Required params: AutoScalingGroupName, MinSize, MaxSize
//
// See http://goo.gl/3S13Bv for more details.
func (as *AutoScaling) CreateLaunchConfiguration(options *CreateLaunchConfiguration) (resp *GenericResp, err error) {

	var b64 = base64.StdEncoding

	params := makeParams("CreateLaunchConfiguration")
	params["LaunchConfigurationName"] = options.LaunchConfigurationName

	if options.AssociatePublicIpAddress {
		params["AssociatePublicIpAddress"] = "true"
	}

	if options.EbsOptimized {
		params["EbsOptimized"] = "true"
	}

	if options.IamInstanceProfile != "" {
		params["IamInstanceProfile"] = options.IamInstanceProfile
	}

	if options.ImageId != "" {
		params["ImageId"] = options.ImageId
	}

	if options.InstanceId != "" {
		params["InstanceId"] = options.InstanceId
	}

	if options.InstanceMonitoring != (InstanceMonitoring{}) {
		params["InstanceMonitoring.Enabled"] = "true"
	}

	if options.InstanceType != "" {
		params["InstanceType"] = options.InstanceType
	}

	if options.KernelId != "" {
		params["KernelId"] = options.KernelId
	}

	if options.KeyName != "" {
		params["KeyName"] = options.KeyName
	}

	if options.RamdiskId != "" {
		params["RamdiskId"] = options.RamdiskId
	}

	if options.SpotPrice != "" {
		params["SpotPrice"] = options.SpotPrice
	}

	if options.UserData != "" {
		params["UserData"] = b64.EncodeToString([]byte(options.UserData))
	}

	for i, bdm := range options.BlockDeviceMappings {
		key := "BlockDeviceMappings.member.%d.%s"
		index := i + 1
		params[fmt.Sprintf(key, index, "DeviceName")] = bdm.DeviceName
		params[fmt.Sprintf(key, index, "VirtualName")] = bdm.VirtualName

		if bdm.NoDevice {
			params[fmt.Sprintf(key, index, "NoDevice")] = "true"
		}

		if bdm.Ebs != (EBS{}) {
			key := "BlockDeviceMappings.member.%d.Ebs.%s"

			//Defaults to true
			params[fmt.Sprintf(key, index, "DeleteOnTermination")] = strconv.FormatBool(bdm.Ebs.DeleteOnTermination)

			if bdm.Ebs.Iops != 0 {
				params[fmt.Sprintf(key, index, "Iops")] = strconv.Itoa(bdm.Ebs.Iops)
			}

			if bdm.Ebs.SnapshotId != "" {
				params[fmt.Sprintf(key, index, "SnapshotId")] = bdm.Ebs.SnapshotId
			}

			if bdm.Ebs.VolumeSize != 0 {
				params[fmt.Sprintf(key, index, "VolumeSize")] = strconv.Itoa(bdm.Ebs.VolumeSize)
			}

			if bdm.Ebs.VolumeType != "" {
				params[fmt.Sprintf(key, index, "VolumeType")] = bdm.Ebs.VolumeType
			}
		}
	}

	for i, sg := range options.SecurityGroups {
		key := fmt.Sprintf("SecurityGroups.member.%d", i+1)
		params[key] = sg
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Creates or Updates Auto Scaling Group Tags
//
// See http://goo.gl/e1UIXb for more details.
func (as *AutoScaling) CreateOrUpdateTags(tags []Tag) (resp *GenericResp, err error) {
	params := makeParams("CreateOrUpdateTags")

	for i, t := range tags {
		key := "Tags.member.%d.%s"
		index := i + 1
		params[fmt.Sprintf(key, index, "Key")] = t.Key
		params[fmt.Sprintf(key, index, "Value")] = t.Value
		params[fmt.Sprintf(key, index, "PropagateAtLaunch")] = strconv.FormatBool(t.PropagateAtLaunch)
		params[fmt.Sprintf(key, index, "ResourceId")] = t.ResourceId
		if t.ResourceType != "" {
			params[fmt.Sprintf(key, index, "ResourceType")] = t.ResourceType
		} else {
			params[fmt.Sprintf(key, index, "ResourceType")] = "auto-scaling-group"
		}
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Deletes an Auto Scaling Group
//
// See http://goo.gl/us7VSffor for more details.
func (as *AutoScaling) DeleteAutoScalingGroup(asgName string, forceDelete bool) (resp *GenericResp, err error) {
	params := makeParams("DeleteAutoScalingGroup")
	params["AutoScalingGroupName"] = asgName

	if forceDelete {
		params["ForceDelete"] = "true"
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Deletes a Launch Configuration
//
// See http://goo.gl/xksfyR for more details.
func (as *AutoScaling) DeleteLaunchConfiguration(name string) (resp *GenericResp, err error) {
	params := makeParams("DeleteLaunchConfiguration")
	params["LaunchConfigurationName"] = name

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Deletes notifications created by PutNotificationConfiguration.
//
// See http://goo.gl/jTqoYz for more details
func (as *AutoScaling) DeleteNotificationConfiguration(asgName string, topicARN string) (resp *GenericResp, err error) {
	params := makeParams("DeleteNotificationConfiguration")
	params["AutoScalingGroupName"] = asgName
	params["TopicARN"] = topicARN

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Deletes a policy created by PutScalingPolicy.
//
// policyName might be the policy name or ARN
//
// See http://goo.gl/aOQPH2 for more details
func (as *AutoScaling) DeletePolicy(asgName string, policyName string) (resp *GenericResp, err error) {
	params := makeParams("DeletePolicy")
	params["AutoScalingGroupName"] = asgName
	params["PolicyName"] = policyName

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Deletes a scheduled action previously created using the PutScheduledUpdateGroupAction.
//
// See http://goo.gl/Zss9CH for more details
func (as *AutoScaling) DeleteScheduledAction(asgName string, scheduledActionName string) (resp *GenericResp, err error) {
	params := makeParams("DeleteScheduledAction")
	params["AutoScalingGroupName"] = asgName
	params["ScheduledActionName"] = scheduledActionName

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Delete Auto Scaling Group Tags
//
// See http://goo.gl/o8HzAk for more details.
func (as *AutoScaling) DeleteTags(tags []Tag) (resp *GenericResp, err error) {
	params := makeParams("DeleteTags")

	for i, t := range tags {
		key := "Tags.member.%d.%s"
		index := i + 1
		params[fmt.Sprintf(key, index, "Key")] = t.Key
		params[fmt.Sprintf(key, index, "Value")] = t.Value
		params[fmt.Sprintf(key, index, "PropagateAtLaunch")] = strconv.FormatBool(t.PropagateAtLaunch)
		params[fmt.Sprintf(key, index, "ResourceId")] = t.ResourceId
		params[fmt.Sprintf(key, index, "ResourceType")] = "auto-scaling-group"
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

//DescribeAccountLimits response wrapper
//
// See http://goo.gl/tKsMN0 for more details.
type DescribeAccountLimitsResp struct {
	MaxNumberOfAutoScalingGroups    int    `xml:"DescribeAccountLimitsResult>MaxNumberOfAutoScalingGroups"`
	MaxNumberOfLaunchConfigurations int    `xml:"DescribeAccountLimitsResult>MaxNumberOfLaunchConfigurations"`
	RequestId                       string `xml:"ResponseMetadata>RequestId"`
}

// DescribeAccountLimits - Returns the limits for the Auto Scaling resources currently allowed for your AWS account.
//
// See http://goo.gl/tKsMN0 for more details.
func (as *AutoScaling) DescribeAccountLimits() (resp *DescribeAccountLimitsResp, err error) {
	params := makeParams("DescribeAccountLimits")

	resp = new(DescribeAccountLimitsResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// AdjustmentType - specifies whether the PutScalingPolicy ScalingAdjustment parameter is an absolute number or a percentage of the current capacity.
//
// See http://goo.gl/tCFqeL for more details
type AdjustmentType struct {
	AdjustmentType string //Valid values are ChangeInCapacity, ExactCapacity, and PercentChangeInCapacity.
}

//DescribeAdjustmentTypes response wrapper
//
// See http://goo.gl/hGx3Pc for more details.
type DescribeAdjustmentTypesResp struct {
	AdjustmentTypes []AdjustmentType `xml:"DescribeAdjustmentTypesResult>AdjustmentTypes>member"`
	RequestId       string           `xml:"ResponseMetadata>RequestId"`
}

// DescribeAdjustmentTypes - Returns policy adjustment types for use in the PutScalingPolicy action.
//
// See http://goo.gl/hGx3Pc for more details.
func (as *AutoScaling) DescribeAdjustmentTypes() (resp *DescribeAdjustmentTypesResp, err error) {
	params := makeParams("DescribeAdjustmentTypes")

	resp = new(DescribeAdjustmentTypesResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

//DescribeAutoScalingGroups response wrapper
//
//See http://goo.gl/nW74Ut for more details.
type DescribeAutoScalingGroupsResp struct {
	AutoScalingGroups []AutoScalingGroup `xml:"DescribeAutoScalingGroupsResult>AutoScalingGroups>member"`
	NextToken         string             `xml:"DescribeAutoScalingGroupsResult>NextToken"`
	RequestId         string             `xml:"ResponseMetadata>RequestId"`
}

// DescribeAutoScalingGroups - Returns a full description of each Auto Scaling group in the given list
// If no autoscaling groups are provided, returns the details of all autoscaling groups
// Supports pagination by using the returned "NextToken" parameter for subsequent calls
//
// See http://goo.gl/nW74Ut for more details.
func (as *AutoScaling) DescribeAutoScalingGroups(names []string, maxRecords int, nextToken string) (resp *DescribeAutoScalingGroupsResp, err error) {
	params := makeParams("DescribeAutoScalingGroups")

	if maxRecords != 0 {
		params["MaxRecords"] = strconv.Itoa(maxRecords)
	}

	if nextToken != "" {
		params["NextToken"] = nextToken
	}

	for i, name := range names {
		index := fmt.Sprintf("AutoScalingGroupNames.member.%d", i+1)
		params[index] = name
	}

	resp = new(DescribeAutoScalingGroupsResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

//DescribeAutoScalingInstances response wrapper
//
//See http://goo.gl/ckzORt for more details.
type DescribeAutoScalingInstancesResp struct {
	AutoScalingInstances []Instance `xml:"DescribeAutoScalingInstancesResult>AutoScalingInstances>member"`
	NextToken            string     `xml:"DescribeAutoScalingInstancesResult>NextToken"`
	RequestId            string     `xml:"ResponseMetadata>RequestId"`
}

// DescribeAutoScalingInstances - Returns a description of each Auto Scaling instance in the InstanceIds list.
// If a list is not provided, the service returns the full details of all instances up to a maximum of 50
// By default, the service returns a list of 20 items.
// Supports pagination by using the returned "NextToken" parameter for subsequent calls
//
// See http://goo.gl/ckzORt for more details.
func (as *AutoScaling) DescribeAutoScalingInstances(ids []string, maxRecords int, nextToken string) (resp *DescribeAutoScalingInstancesResp, err error) {
	params := makeParams("DescribeAutoScalingInstances")

	if maxRecords != 0 {
		params["MaxRecords"] = strconv.Itoa(maxRecords)
	}

	if nextToken != "" {
		params["NextToken"] = nextToken
	}

	for i, id := range ids {
		index := fmt.Sprintf("InstanceIds.member.%d", i+1)
		params[index] = id
	}

	resp = new(DescribeAutoScalingInstancesResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

//DescribeAutoScalingNotificationTypes response wrapper
//
// See http://goo.gl/pmLIoE for more details.
type DescribeAutoScalingNotificationTypesResp struct {
	AutoScalingNotificationTypes []string `xml:"DescribeAutoScalingNotificationTypesResult>AutoScalingNotificationTypes>member"`
	RequestId                    string   `xml:"ResponseMetadata>RequestId"`
}

// DescribeAutoScalingNotificationTypes - Returns a list of all notification types that are supported by Auto Scaling
//
// See http://goo.gl/pmLIoE for more details.
func (as *AutoScaling) DescribeAutoScalingNotificationTypes() (resp *DescribeAutoScalingNotificationTypesResp, err error) {
	params := makeParams("DescribeAutoScalingNotificationTypes")

	resp = new(DescribeAutoScalingNotificationTypesResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// LaunchConfiguration - Encapsulates the LaunchConfiguration Data Type
//
// See http://goo.gl/TOJunp
type LaunchConfiguration struct {
	AssociatePublicIpAddress bool
	BlockDeviceMappings      []BlockDeviceMapping `xml:"BlockDeviceMappings>member"`
	CreatedTime              time.Time
	EbsOptimized             bool
	IamInstanceProfile       string
	ImageId                  string
	InstanceId               string
	InstanceMonitoring       InstanceMonitoring
	InstanceType             string
	KernelId                 string
	KeyName                  string
	LaunchConfigurationARN   string
	LaunchConfigurationName  string
	RamdiskId                string
	SecurityGroups           []string `xml:"SecurityGroups>member"`
	SpotPrice                string
	UserData                 string `xml:"UserData"`
}

// DescribeLaunchConfigurations response wrapper
//
// See http://goo.gl/y31YYE for more details.
type DescribeLaunchConfigurationsResp struct {
	LaunchConfigurations []LaunchConfiguration `xml:"DescribeLaunchConfigurationsResult>LaunchConfigurations>member"`
	NextToken            string                `xml:"DescribeLaunchConfigurationsResult>NextToken"`
	RequestId            string                `xml:"ResponseMetadata>RequestId"`
}

// DescribeLaunchConfigurations - Returns a full description of all launch configurations, or the specified launch configurations.
//
// http://goo.gl/y31YYE for more details.
func (as *AutoScaling) DescribeLaunchConfigurations(names []string, maxRecords int, nextToken string) (resp *DescribeLaunchConfigurationsResp, err error) {
	params := makeParams("DescribeLaunchConfigurations")

	if maxRecords != 0 {
		params["MaxRecords"] = strconv.Itoa(maxRecords)
	}

	if nextToken != "" {
		params["NextToken"] = nextToken
	}

	for i, name := range names {
		index := fmt.Sprintf("LaunchConfigurationNames.member.%d", i+1)
		params[index] = name
	}

	resp = new(DescribeLaunchConfigurationsResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

//MetricGranularity - Encapsulates the MetricGranularityType
//
// See http://goo.gl/WJ82AA for more details
type MetricGranularity struct {
	Granularity string
}

//MetricCollection - Encapsulates the MetricCollectionType
//
// See http://goo.gl/YrEG6h for more details
type MetricCollection struct {
	Metric string
}

//DescribeMetricCollectionTypesResp response wrapper
//
// See http://goo.gl/UyYc3i for more details.
type DescribeMetricCollectionTypesResp struct {
	Granularities []MetricGranularity `xml:"DescribeMetricCollectionTypesResult>Granularities>member"`
	Metrics       []MetricCollection  `xml:"DescribeMetricCollectionTypesResult>Metrics>member"`
	RequestId     string              `xml:"ResponseMetadata>RequestId"`
}

// DescribeMetricCollectionTypes - Returns a list of metrics and a corresponding list of granularities for each metric
//
// See http://goo.gl/UyYc3i for more details.
func (as *AutoScaling) DescribeMetricCollectionTypes() (resp *DescribeMetricCollectionTypesResp, err error) {
	params := makeParams("DescribeMetricCollectionTypes")

	resp = new(DescribeMetricCollectionTypesResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

//NotificationConfiguration - Encapsulates the NotificationConfigurationType
//
// See http://goo.gl/M8xYOQ for more details
type NotificationConfiguration struct {
	AutoScalingGroupName string
	NotificationType     string
	TopicARN             string
}

// DescribeNotificationConfigurations response wrapper
//
// See http://goo.gl/qiAH31 for more details.
type DescribeNotificationConfigurationsResp struct {
	NotificationConfigurations []NotificationConfiguration `xml:"DescribeNotificationConfigurationsResult>NotificationConfigurations>member"`
	NextToken                  string                      `xml:"DescribeNotificationConfigurationsResult>NextToken"`
	RequestId                  string                      `xml:"ResponseMetadata>RequestId"`
}

// DescribeNotificationConfigurations - Returns a list of notification actions associated with Auto Scaling groups for specified events.
// Supports pagination by using the returned "NextToken" parameter for subsequent calls
//
// http://goo.gl/qiAH31 for more details.
func (as *AutoScaling) DescribeNotificationConfigurations(asgNames []string, maxRecords int, nextToken string) (resp *DescribeNotificationConfigurationsResp, err error) {
	params := makeParams("DescribeNotificationConfigurations")

	if maxRecords != 0 {
		params["MaxRecords"] = strconv.Itoa(maxRecords)
	}

	if nextToken != "" {
		params["NextToken"] = nextToken
	}

	for i, name := range asgNames {
		index := fmt.Sprintf("AutoScalingGroupNames.member.%d", i+1)
		params[index] = name
	}

	resp = new(DescribeNotificationConfigurationsResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Alarm - Encapsulates the Alarm data type.
//
// See http://goo.gl/Q0uPAB for more details
type Alarm struct {
	AlarmARN  string
	AlarmName string
}

// ScalingPolicy - Encapsulates the ScalingPolicyType
//
// See http://goo.gl/BYAT18 for more details
type ScalingPolicy struct {
	AdjustmentType       string  // ChangeInCapacity, ExactCapacity, and PercentChangeInCapacity
	Alarms               []Alarm `xml:"Alarms>member"` //A list of CloudWatch Alarms related to the policy
	AutoScalingGroupName string
	Cooldown             int
	MinAdjustmentStep    int // Changes the DesiredCapacity of ASG by at least the specified number of instances.
	PolicyARN            string
	PolicyName           string
	ScalingAdjustment    int
}

// DescribePolicies response wrapper
//
// http://goo.gl/bN7A9T for more details.
type DescribePoliciesResp struct {
	ScalingPolicies []ScalingPolicy `xml:"DescribePoliciesResult>ScalingPolicies>member"`
	NextToken       string          `xml:"DescribePoliciesResult>NextToken"`
	RequestId       string          `xml:"ResponseMetadata>RequestId"`
}

// DescribePolicies - Returns descriptions of what each policy does.
// Supports pagination by using the returned "NextToken" parameter for subsequent calls
//
// http://goo.gl/bN7A9Tfor more details.
func (as *AutoScaling) DescribePolicies(asgName string, policyNames []string, maxRecords int, nextToken string) (resp *DescribePoliciesResp, err error) {
	params := makeParams("DescribePolicies")

	if asgName != "" {
		params["AutoScalingGroupName"] = asgName
	}

	if maxRecords != 0 {
		params["MaxRecords"] = strconv.Itoa(maxRecords)
	}

	if nextToken != "" {
		params["NextToken"] = nextToken
	}

	for i, name := range policyNames {
		index := fmt.Sprintf("PolicyNames.member.%d", i+1)
		params[index] = name
	}

	resp = new(DescribePoliciesResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Activity - Encapsulates the Activity data type
//
// See http://goo.gl/fRaVi1 for more details
type Activity struct {
	ActivityId           string
	AutoScalingGroupName string
	Cause                string
	Description          string
	Details              string
	EndTime              time.Time
	Progress             int
	StartTime            time.Time
	StatusCode           string
	StatusMessage        string
}

// DescribeScalingActivities response wrapper
//
// http://goo.gl/noOXIC for more details.
type DescribeScalingActivitiesResp struct {
	Activities []Activity `xml:"DescribeScalingActivitiesResult>Activities>member"`
	NextToken  string     `xml:"DescribeScalingActivitiesResult>NextToken"`
	RequestId  string     `xml:"ResponseMetadata>RequestId"`
}

// DescribeScalingActivities - Returns the scaling activities for the specified Auto Scaling group.
// Supports pagination by using the returned "NextToken" parameter for subsequent calls
//
// http://goo.gl/noOXIC more details.
func (as *AutoScaling) DescribeScalingActivities(asgName string, activityIds []string, maxRecords int, nextToken string) (resp *DescribeScalingActivitiesResp, err error) {
	params := makeParams("DescribeScalingActivities")

	if asgName != "" {
		params["AutoScalingGroupName"] = asgName
	}

	if maxRecords != 0 {
		params["MaxRecords"] = strconv.Itoa(maxRecords)
	}

	if nextToken != "" {
		params["NextToken"] = nextToken
	}

	for i, id := range activityIds {
		index := fmt.Sprintf("ActivityIds.member.%d", i+1)
		params[index] = id
	}

	resp = new(DescribeScalingActivitiesResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Encapsulates the Auto Scaling process data type
//
// See http://goo.gl/9BvNik for more details.
type ProcessType struct {
	ProcessName string
}

// DescribeScalingProcessTypes response wrapper
//
// See http://goo.gl/rkp2tw for more details.
type DescribeScalingProcessTypesResp struct {
	Processes []ProcessType `xml:"DescribeScalingProcessTypesResult>Processes>member"`
	RequestId string        `xml:"ResponseMetadata>RequestId"`
}

// DescribeScalingProcessTypes - Returns scaling process types for use in the ResumeProcesses and SuspendProcesses actions.
//
// See http://goo.gl/rkp2tw for more details.
func (as *AutoScaling) DescribeScalingProcessTypes() (resp *DescribeScalingProcessTypesResp, err error) {
	params := makeParams("DescribeScalingProcessTypes")

	resp = new(DescribeScalingProcessTypesResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ScheduledUpdateGroupAction - Encapsulates the ScheduledUpdateGroupAction data type
//
// See http://goo.gl/z2Kfxe for more details
type ScheduledUpdateGroupAction struct {
	AutoScalingGroupName string
	DesiredCapacity      int
	EndTime              time.Time
	MaxSize              int
	MinSize              int
	Recurrence           string
	ScheduledActionARN   string
	ScheduledActionName  string
	StartTime            time.Time
	Time                 time.Time
}

// DescribeScheduledActions response wrapper
//
// See http://goo.gl/zqrJLx for more details.
type DescribeScheduledActionsResp struct {
	ScheduledUpdateGroupActions []ScheduledUpdateGroupAction `xml:"DescribeScheduledActionsResult>ScheduledUpdateGroupActions>member"`
	NextToken                   string                       `xml:"DescribeScheduledActionsResult>NextToken"`
	RequestId                   string                       `xml:"ResponseMetadata>RequestId"`
}

// DescribeScheduledActions - Lists all the actions scheduled for your Auto Scaling group that haven't been executed.
// Supports pagination by using the returned "NextToken" parameter for subsequent calls
//
// See http://goo.gl/zqrJLx for more details.
func (as *AutoScaling) DescribeScheduledActions(asgName string, actionNames []string, sTime time.Time, eTime time.Time, maxRecords int, nextToken string) (resp *DescribeScheduledActionsResp, err error) {
	params := makeParams("DescribeScheduledActions")

	if asgName != "" {
		params["AutoScalingGroupName"] = asgName
	}

	if maxRecords != 0 {
		params["MaxRecords"] = strconv.Itoa(maxRecords)
	}

	if nextToken != "" {
		params["NextToken"] = nextToken
	}

	if !eTime.IsZero() {
		params["EndTime"] = eTime.Format(time.RFC3339)
	}

	if sTime.IsZero() {
		params["StartTime"] = sTime.Format(time.RFC3339)
	}

	for i, name := range actionNames {
		index := fmt.Sprintf("ScheduledActionNames.member.%d", i+1)
		params[index] = name
	}

	resp = new(DescribeScheduledActionsResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// DescribeTags response wrapper
//
// See http://goo.gl/ZTEU3G for more details.
type DescribeTagsResp struct {
	Tags      []Tag  `xml:"DescribeTagsResult>Tags>member"`
	NextToken string `xml:"DescribeTagsResult>NextToken"`
	RequestId string `xml:"ResponseMetadata>RequestId"`
}

// DescribeTags - Lists the Auto Scaling group tags.
// Supports pagination by using the returned "NextToken" parameter for subsequent calls
//
// See http://goo.gl/ZTEU3G for more details.
func (as *AutoScaling) DescribeTags(filter *Filter, maxRecords int, nextToken string) (resp *DescribeTagsResp, err error) {
	params := makeParams("DescribeTags")

	if maxRecords != 0 {
		params["MaxRecords"] = strconv.Itoa(maxRecords)
	}

	if nextToken != "" {
		params["NextToken"] = nextToken
	}

	filter.addParams(params)

	resp = new(DescribeTagsResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// DescribeTerminationPolicyTypes response wrapper
//
// See http://goo.gl/ZTEU3G for more details.
type DescribeTerminationPolicyTypesResp struct {
	TerminationPolicyTypes []string `xml:"DescribeTerminationPolicyTypesResult>TerminationPolicyTypes>member"`
	RequestId              string   `xml:"ResponseMetadata>RequestId"`
}

// DescribeTerminationPolicyTypes - Returns a list of all termination policies supported by Auto Scaling
//
// See http://goo.gl/ZTEU3G for more details.
func (as *AutoScaling) DescribeTerminationPolicyTypes() (resp *DescribeTerminationPolicyTypesResp, err error) {
	params := makeParams("DescribeTerminationPolicyTypes")

	resp = new(DescribeTerminationPolicyTypesResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// DisableMetricsCollection - Disables monitoring of group metrics for the Auto Scaling group specified in asgName.
// You can specify the list of affected metrics with the metrics parameter. If no metrics are specified, all metrics are disabled
//
// See http://goo.gl/kAvzQw for more details.
func (as *AutoScaling) DisableMetricsCollection(asgName string, metrics []string) (resp *GenericResp, err error) {
	params := makeParams("DisableMetricsCollection")
	params["AutoScalingGroupName"] = asgName

	for i, metric := range metrics {
		index := fmt.Sprintf("Metrics.member.%d", i+1)
		params[index] = metric
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// EnableMetricsCollection - Enables monitoring of group metrics for the Auto Scaling group specified in asNmae.
// You can specify the list of affected metrics with the metrics parameter.
// Auto Scaling metrics collection can be turned on only if the InstanceMonitoring flag is set to true.
// Currently, the only legal granularity is "1Minute".
//
// See http://goo.gl/UcVDWn for more details.
func (as *AutoScaling) EnableMetricsCollection(asgName string, metrics []string, granularity string) (resp *GenericResp, err error) {
	params := makeParams("EnableMetricsCollection")
	params["AutoScalingGroupName"] = asgName
	params["Granularity"] = granularity

	for i, metric := range metrics {
		index := fmt.Sprintf("Metrics.member.%d", i+1)
		params[index] = metric
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ExecutePolicy - Executes the specified policy.
//
// See http://goo.gl/BxHpFc for more details.
func (as *AutoScaling) ExecutePolicy(policyName string, asgName string, honorCooldown bool) (resp *GenericResp, err error) {
	params := makeParams("ExecutePolicy")
	params["PolicyName"] = policyName

	if asgName != "" {
		params["AutoScalingGroupName"] = asgName
	}

	if honorCooldown {
		params["HonorCooldown"] = "true"
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// PutNotificationConfiguration - Configures an Auto Scaling group to send notifications when specified events take place.
//
// See http://goo.gl/9XrROq for more details.
func (as *AutoScaling) PutNotificationConfiguration(asgName string, notificationTypes []string, topicARN string) (resp *GenericResp, err error) {
	params := makeParams("PutNotificationConfiguration")
	params["AutoScalingGroupName"] = asgName
	params["TopicARN"] = topicARN

	for i, n := range notificationTypes {
		index := fmt.Sprintf("NotificationTypes.member.%d", i+1)
		params[index] = n
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// PutScalingPolicy response wrapper
//
// See http://goo.gl/o0E8hl for more details.
type PutScalingPolicyResp struct {
	PolicyARN string `xml:"PutScalingPolicyResult>PolicyARN"`
	RequestId string `xml:"ResponseMetadata>RequestId"`
}

// PutScalingPolicy - Creates or updates a policy for an Auto Scaling group
//
// See http://goo.gl/o0E8hl for more details.
func (as *AutoScaling) PutScalingPolicy(asgName string, policyName string, scalingAdj int, aType string, cooldown int, minAdjStep int) (resp *PutScalingPolicyResp, err error) {
	params := makeParams("PutScalingPolicy")
	params["AutoScalingGroupName"] = asgName
	params["PolicyName"] = policyName
	params["ScalingAdjustment"] = strconv.Itoa(scalingAdj)
	params["AdjustmentType"] = aType

	if cooldown != 0 {
		params["Cooldown"] = strconv.Itoa(cooldown)
	}

	if minAdjStep != 0 {
		params["MinAdjustmentStep"] = strconv.Itoa(minAdjStep)
	}

	resp = new(PutScalingPolicyResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// PutScheduledUpdateGroupAction encapsulates the options for the respective request
//
// See http://goo.gl/sLPi0d for more details
type PutScheduledUpdateGroupAction struct {
	AutoScalingGroupName string
	DesiredCapacity      int
	EndTime              time.Time
	MaxSize              int
	MinSize              int
	Recurrence           string
	ScheduledActionName  string
	StartTime            time.Time
}

// PutScheduledUpdateGroupAction - Creates or updates a scheduled scaling action for an Auto Scaling group.
// When updating a scheduled scaling action, if you leave a parameter unspecified, the corresponding value remains unchanged in the affected Auto Scaling group.
//
// See http://goo.gl/sLPi0d for more details.
func (as *AutoScaling) PutScheduledUpdateGroupAction(options *PutScheduledUpdateGroupAction) (resp *GenericResp, err error) {
	params := makeParams("PutScheduledUpdateGroupAction")
	params["AutoScalingGroupName"] = options.AutoScalingGroupName
	params["ScheduledActionName"] = options.ScheduledActionName

	if options.DesiredCapacity != 0 {
		params["DesiredCapacity"] = strconv.Itoa(options.DesiredCapacity)
	}

	if !options.StartTime.IsZero() {
		params["StartTime"] = options.StartTime.Format(time.RFC3339)
	}

	if !options.EndTime.IsZero() {
		params["EndTime"] = options.EndTime.Format(time.RFC3339)
	}

	if options.MinSize != 0 {
		params["MinSize"] = strconv.Itoa(options.MinSize)
	}

	if options.MaxSize != 0 {
		params["MaxSize"] = strconv.Itoa(options.MaxSize)
	}

	if options.Recurrence != "" {
		params["Recurrence"] = options.Recurrence
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ResumeProcesses - Resumes all suspended Auto Scaling processes for an Auto Scaling group.
//
// See http://goo.gl/XWIIg1 for more details.
func (as *AutoScaling) ResumeProcesses(asgName string, scalingProcesses []string) (resp *GenericResp, err error) {
	params := makeParams("ResumeProcesses")
	params["AutoScalingGroupName"] = asgName

	for i, s := range scalingProcesses {
		index := fmt.Sprintf("ScalingProcesses.member.%d", i+1)
		params[index] = s
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// SetDesiredCapacity - Sets the desired size of the specified AutoScalingGroup
//
// See http://goo.gl/3WGZbI for more details.
func (as *AutoScaling) SetDesiredCapacity(asgName string, desiredCapacity int, honorCooldown bool) (resp *GenericResp, err error) {
	params := makeParams("SetDesiredCapacity")
	params["AutoScalingGroupName"] = asgName
	params["DesiredCapacity"] = strconv.Itoa(desiredCapacity)

	if honorCooldown {
		params["HonorCooldown"] = "true"
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// SetInstanceHealth - Sets the health status of a specified instance that belongs to any of your Auto Scaling groups.
//
// See http://goo.gl/j4ZRxh for more details.
func (as *AutoScaling) SetInstanceHealth(id string, healthStatus string, respectGracePeriod bool) (resp *GenericResp, err error) {
	params := makeParams("SetInstanceHealth")
	params["HealthStatus"] = healthStatus
	params["InstanceId"] = id

	//Default is true
	if !respectGracePeriod {
		params["ShouldRespectGracePeriod"] = "false"
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// SuspendProcesses - Suspends Auto Scaling processes for an Auto Scaling group.
//
// See http://goo.gl/DUJpQy for more details.
func (as *AutoScaling) SuspendProcesses(asgName string, scalingProcesses []string) (resp *GenericResp, err error) {
	params := makeParams("SuspendProcesses")
	params["AutoScalingGroupName"] = asgName

	for i, s := range scalingProcesses {
		index := fmt.Sprintf("ScalingProcesses.member.%d", i+1)
		params[index] = s
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// TerminateInstanceInAutoScalingGroupResp response wrapper
//
// See http://goo.gl/ki5hMh for more details.
type TerminateInstanceInAutoScalingGroupResp struct {
	Activity  Activity `xml:"TerminateInstanceInAutoScalingGroupResult>Activity"`
	RequestId string   `xml:"ResponseMetadata>RequestId"`
}

// TerminateInstanceInAutoScalingGroup - Suspends Auto Scaling processes for an Auto Scaling group.
// decrCap - Specifies whether terminating this instance should also decrement the size of the Auto Scaling Group
//
// See http://goo.gl/ki5hMh for more details.
func (as *AutoScaling) TerminateInstanceInAutoScalingGroup(id string, decrCap bool) (resp *TerminateInstanceInAutoScalingGroupResp, err error) {
	params := makeParams("TerminateInstanceInAutoScalingGroup")
	params["InstanceId"] = id
	params["ShouldDecrementDesiredCapacity"] = strconv.FormatBool(decrCap)

	resp = new(TerminateInstanceInAutoScalingGroupResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// The UpdateAutoScalingGroup type encapsulates options for the respective request.
//
// See http://goo.gl/rqrmxy for more details.
type UpdateAutoScalingGroup struct {
	AutoScalingGroupName    string
	AvailabilityZones       []string
	DefaultCooldown         int
	DesiredCapacity         int
	HealthCheckGracePeriod  int
	HealthCheckType         string
	InstanceId              string
	LaunchConfigurationName string
	MaxSize                 int
	MinSize                 int
	PlacementGroup          string
	TerminationPolicies     []string
	VPCZoneIdentifier       string
}

// UpdateAutoScalingGroup - Updates the configuration for the specified AutoScalingGroup.
//
// See http://goo.gl/rqrmxy for more details.
func (as *AutoScaling) UpdateAutoScalingGroup(options *UpdateAutoScalingGroup) (resp *GenericResp, err error) {
	params := makeParams("UpdateAutoScalingGroup")

	params["AutoScalingGroupName"] = options.AutoScalingGroupName
	params["MaxSize"] = strconv.Itoa(options.MaxSize)
	params["MinSize"] = strconv.Itoa(options.MinSize)
	params["DesiredCapacity"] = strconv.Itoa(options.DesiredCapacity)

	if options.DefaultCooldown != 0 {
		params["DefaultCooldown"] = strconv.Itoa(options.DefaultCooldown)
	}

	if options.HealthCheckGracePeriod != 0 {
		params["HealthCheckGracePeriod"] = strconv.Itoa(options.HealthCheckGracePeriod)
	}

	if options.HealthCheckType != "" {
		params["HealthCheckType"] = options.HealthCheckType
	}

	if options.InstanceId != "" {
		params["InstanceId"] = options.InstanceId
	}

	if options.LaunchConfigurationName != "" {
		params["LaunchConfigurationName"] = options.LaunchConfigurationName
	}

	if options.PlacementGroup != "" {
		params["PlacementGroup"] = options.PlacementGroup
	}

	if options.VPCZoneIdentifier != "" {
		params["VPCZoneIdentifier"] = options.VPCZoneIdentifier
	}

	for i, az := range options.AvailabilityZones {
		key := fmt.Sprintf("AvailabilityZones.member.%d", i+1)
		params[key] = az
	}

	for i, tp := range options.TerminationPolicies {
		key := fmt.Sprintf("TerminationPolicies.member.%d", i+1)
		params[key] = tp
	}

	resp = new(GenericResp)
	if err := as.query(params, resp); err != nil {
		return nil, err
	}
	return resp, nil
}
