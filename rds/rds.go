package rds

import (
	"encoding/xml"
	"github.com/crowdmob/goamz/aws"
	"log"
	"net/http/httputil"
	"strconv"
)

const debug = true

const (
	ServiceName = "rds"
	ApiVersion  = "2013-09-09"
)

// The RDS type encapsulates operations within a specific EC2 region.
type RDS struct {
	Service aws.AWSService
}

// New creates a new RDS Client.
func New(auth aws.Auth, region aws.Region) (*RDS, error) {
	service, err := aws.NewService(auth, region.RDSEndpoint)
	if err != nil {
		return nil, err
	}
	return &RDS{
		Service: service,
	}, nil
}

// ----------------------------------------------------------------------------
// Request dispatching logic.

// query dispatches a request to the RDS API signed with a version 2 signature
func (rds *RDS) query(method, path string, params map[string]string, resp interface{}) error {
	// Add basic RDS param
	params["Version"] = ApiVersion

	r, err := rds.Service.Query(method, path, params)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	if debug {
		dump, _ := httputil.DumpResponse(r, true)
		log.Printf("response:\n")
		log.Printf("%v\n}\n", string(dump))
	}

	if r.StatusCode != 200 {
		return rds.Service.BuildError(r)
	}
	err = xml.NewDecoder(r.Body).Decode(resp)
	return err
}

// ----------------------------------------------------------------------------
// API methods and corresponding response types.

// Response to a DescribeDBInstances request
//
// See http://goo.gl/KSPlAl for more details.
type DescribeDBInstancesResponse struct {
	DBInstances []DBInstance `xml:"DescribeDBInstancesResult>DBInstances>DBInstance"` // The list of database instances
	Marker      string       `xml:"DescribeDBInstancesResult>Marker"`                 // An optional pagination token provided by a previous request
	RequestId   string       `xml:"ResponseMetadata>RequestId"`
}

// DescribeDBInstances - Returns a description of each Database Instance
// Supports pagination by using the "Marker" parameter, and "maxRecords" for subsequent calls
// Unfortunately RDS does not currently support filtering
//
// See http://goo.gl/lzZMyz for more details.
func (rds *RDS) DescribeDBInstances(id string, maxRecords int, marker string) (*DescribeDBInstancesResponse, error) {

	params := aws.MakeParams("DescribeDBInstances")

	if id != "" {
		params["DBInstanceIdentifier"] = id
	}

	if maxRecords != 0 {
		params["MaxRecords"] = strconv.Itoa(maxRecords)
	}
	if marker != "" {
		params["Marker"] = marker
	}

	resp := &DescribeDBInstancesResponse{}
	err := rds.query("POST", "/", params, resp)
	return resp, err
}

// CreateDBInstanceOptions describes the options used to create a new Database Instance
//
// See http://goo.gl/yFxFL9 for more details.
type CreateDBInstanceOptions struct {
	AllocatedStorage           int      // Specifies the allocated storage size specified in gigabytes.
	AutoMinorVersionUpgrade    bool     // Indicates that minor version patches are applied automatically.
	AvailabilityZone           string   // Specifies the name of the Availability Zone the DB instance is located in.
	BackupRetentionPeriod      int      // Specifies the number of days for which automatic DB snapshots are retained.
	CharacterSetName           string   // If present, specifies the name of the character set that this instance is associated with.
	DBInstanceClass            string   // Contains the name of the compute and memory capacity class of the DB instance.
	DBInstanceIdentifier       string   // Contains a user-supplied database identifier. This is the unique key that identifies a DB instance.
	DBName                     string   // The meaning of this parameter differs according to the database engine you use.
	DBParameterGroupName       string   // The name of a DB parameter group to be associated with this DB instance
	DBSecurityGroups           []string // A list of DB security group IDs to associate with this DB instance
	DBSubnetGroupName          string   // A DB subnet group to associate with this DB instance
	Engine                     string   // Provides the name of the database engine to be used for this DB instance.
	EngineVersion              string   // Indicates the database engine version.
	Iops                       int      // Specifies the Provisioned IOPS (I/O operations per second) value.
	LicenseModel               string   // License model information for this DB instance.
	MasterUserPassword         string   // The password for the master database user. Can be any printable ASCII character except "/", """, or "@"
	MasterUsername             string   // Contains the master username for the DB instance.
	MultiAZ                    bool     // Specifies if the DB instance is a Multi-AZ deployment.
	OptionGroupName            string   // Provides the list of option group memberships for this DB instance.
	Port                       int      // The port to listen on
	PreferredBackupWindow      string   // Specifies the daily time range during which automated backups are created if automated backups are enabled, as determined by the BackupRetentionPeriod.
	PreferredMaintenanceWindow string   // Specifies the weekly time range (in UTC) during which system maintenance can occur.
	PubliclyAccessible         bool     // Specifies the accessibility options for the DB instance. A value of true specifies an Internet-facing instance with a publicly resolvable DNS name, which resolves to a public IP address. A value of false specifies an internal instance with a DNS name that resolves to a private IP address.
	VpcSecurityGroupIds        []string // A list of EC2 VPC security groups to associate with this DB instance.
}

// Response to a CreateDBInstance request
//
// See http://goo.gl/yFxFL9 for more details.
type CreateDBInstanceResponse struct {
	DBInstance DBInstance `xml:"CreateDBInstanceResult>DBInstance"`
	RequestId  string     `xml:"ResponseMetadata>RequestId"`
}

// CreateDBInstance starts a new database instance in RDS.
//
// See http://goo.gl/yFxFL9 for more details.
func (rds *RDS) CreateDBInstance(options *CreateDBInstanceOptions) (resp *CreateDBInstanceResponse, err error) {
	params := aws.MakeParams("CreateDBInstance")

	if options.AllocatedStorage != 0 {
		params["AllocatedStorage"] = strconv.Itoa(options.AllocatedStorage)
	}
	params["AutoMinorVersionUpgrade"] = strconv.FormatBool(options.AutoMinorVersionUpgrade)
	if options.AvailabilityZone != "" {
		params["AvailabilityZone"] = options.AvailabilityZone
	}
	if options.BackupRetentionPeriod != 0 {
		params["BackupRetentionPeriod"] = strconv.Itoa(options.BackupRetentionPeriod)
	}
	if options.CharacterSetName != "" {
		params["CharacterSetName"] = options.CharacterSetName
	}
	if options.DBInstanceClass != "" {
		params["DBInstanceClass"] = options.DBInstanceClass
	}
	if options.DBInstanceIdentifier != "" {
		params["DBInstanceIdentifier"] = options.DBInstanceIdentifier
	}
	if options.DBName != "" {
		params["DBName"] = options.DBName
	}
	if options.DBParameterGroupName != "" {
		params["DBParameterGroupName"] = options.DBParameterGroupName
	}
	for i, sg := range options.DBSecurityGroups {
		if sg != "" {
			params["DBSecurityGroups.member."+strconv.Itoa(i+1)] = sg
		}
	}
	if options.DBSubnetGroupName != "" {
		params["DBSubnetGroupName"] = options.DBSubnetGroupName
	}
	if options.Engine != "" {
		params["Engine"] = options.Engine
	}
	if options.EngineVersion != "" {
		params["EngineVersion"] = options.EngineVersion
	}
	if options.Iops != 0 {
		params["Iops"] = strconv.Itoa(options.Iops)
	}
	if options.LicenseModel != "" {
		params["LicenseModel"] = options.LicenseModel
	}
	if options.MasterUserPassword != "" {
		params["MasterUserPassword"] = options.MasterUserPassword
	}
	if options.MasterUsername != "" {
		params["MasterUsername"] = options.MasterUsername
	}
	params["MultiAZ"] = strconv.FormatBool(options.MultiAZ)
	if options.OptionGroupName != "" {
		params["OptionGroupName"] = options.OptionGroupName
	}
	if options.Port != 0 {
		params["Port"] = strconv.Itoa(options.Port)
	}
	if options.PreferredBackupWindow != "" {
		params["PreferredBackupWindow"] = options.PreferredBackupWindow
	}
	if options.PreferredMaintenanceWindow != "" {
		params["PreferredMaintenanceWindow"] = options.PreferredMaintenanceWindow
	}
	params["PubliclyAccessible"] = strconv.FormatBool(options.PubliclyAccessible)

	for i, g := range options.VpcSecurityGroupIds {
		if g != "" {
			params["VpcSecurityGroupIds.member."+strconv.Itoa(i+1)] = g
		}
	}

	resp = &CreateDBInstanceResponse{}
	err = rds.query("POST", "/", params, resp)
	if err != nil {
		return nil, err
	}
	return

}

// Response to a DeleteDBInstance request
//
// See http://goo.gl/P6xuwf for more details.
type DeleteDBInstanceResponse struct {
	DBInstance DBInstance `xml:"DeleteDBInstanceResult>DBInstance"`
	RequestId  string     `xml:"ResponseMetadata>RequestId"`
}

// DeleteDBInstance deletes a previously provisioned DB instance
// A successful response from the web service indicates the request was received correctly.
// When you delete a DB instance, all automated backups for that instance are deleted and
// cannot be recovered. Manual DB snapshots of the DB instance to be deleted are not deleted.
//
// If a final DB snapshot is requested the status of the RDS instance will be "deleting"
// until the DB snapshot is created. The API action DescribeDBInstance is used to monitor
// the status of this operation. The action cannot be canceled or reverted once submitted.
//
// See http://goo.gl/P6xuwf for more details.
func (rds *RDS) DeleteDBInstance(id string, finalDBSnapshotIdentifier string, skipFinalSnapshot bool) (resp *DeleteDBInstanceResponse, err error) {
	params := aws.MakeParams("DeleteDBInstance")

	params["DBInstanceIdentifier"] = id
	if finalDBSnapshotIdentifier != "" {
		params["FinalDBSnapshotIdentifier"] = finalDBSnapshotIdentifier
	}
	params["SkipFinalSnapshot"] = strconv.FormatBool(skipFinalSnapshot)

	resp = &DeleteDBInstanceResponse{}
	err = rds.query("POST", "/", params, resp)
	if err != nil {
		return nil, err
	}
	return
}
