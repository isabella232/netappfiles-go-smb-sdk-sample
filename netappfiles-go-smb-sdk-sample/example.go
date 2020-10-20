// Copyright (c) Microsoft and contributors.  All rights reserved.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// This sample code creates an Azure Netapp Files Account, a Capacity Pool,
// and two volumes, one NFSv3 and one NFSv4.1, then it takes a snapshot
// of the first volume (NFSv3) and performs clean up if the variable
// shouldCleanUp is changed to true.
//
// This package uses go-haikunator package (https://github.com/yelinaung/go-haikunator)
// port from Python's haikunator module and therefore used here just for sample simplification,
// this doesn't mean that it is endorsed/thouroughly tested by any means, use at own risk.
// Feel free to provide your own names on variables using it.

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Azure-Samples/netappfiles-go-smb-sdk-sample/netappfiles-go-smb-sdk-sample/internal/sdkutils"
	"github.com/Azure-Samples/netappfiles-go-smb-sdk-sample/netappfiles-go-smb-sdk-sample/internal/utils"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/netapp/mgmt/netapp"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/yelinaung/go-haikunator"
)

const (
	virtualNetworksApiVersion string = "2019-09-01"
)

var (
	shouldCleanUp         bool   = false
	location              string = "westus"
	resourceGroupName     string = "anf-smb-rg"
	vnetResourceGroupName string = "anf-smb-rg"
	vnetName              string = "westus-vnet01"
	subnetName            string = "anf-sn"
	anfAccountName        string = haikunator.New(time.Now().UTC().UnixNano()).Haikunate()
	capacityPoolName      string = "Pool01"
	serviceLevel          string = "Standard"       // Valid service levels are Standard, Premium and Ultra
	capacityPoolSizeBytes int64  = 4398046511104    // 4TiB (minimum capacity pool size)
	volumeSizeBytes       int64  = 107374182400     // 100GiB (minimum volume size)
	protocolTypes                = []string{"CIFS"} // Multiple NFS protocol types are not supported at the moment this sample was written
	smbVolumeName         string = fmt.Sprintf("SMB-Vol-%v-%v", anfAccountName, capacityPoolName)
	sampleTags                   = map[string]*string{
		"Author":  to.StringPtr("ANF Go SMB SDK Sample"),
		"Service": to.StringPtr("Azure Netapp Files"),
	}

	// SMB related variables
	domainJoinUserName     = "pmcadmin"
	domainJoinUserPassword = ""          // **Leave this blank since the user will be prompted to provide this password at the begining**
	dnsList                = "10.2.0.4"  // Please notice that this is a comma-separated string
	adFQDN                 = "anf.local" // FQDN of the Domain where the smb server will be created/domain joined
	smbServerNamePrefix    = "pmc03"     // This needs to be maximum 10 characters in length and during the domain join process a random string gets appended.

	exitCode       int
	smbVolumeID    string = ""
	capacityPoolID string = ""
	acccountID     string = ""
)

func main() {

	cntx := context.Background()

	// Cleanup and exit handling
	defer func() { exit(cntx); os.Exit(exitCode) }()

	utils.PrintHeader("Azure NetAppFiles Go SMB SDK Sample - sample application that creates an SMB volume together with Account and Capacity Pool.")

	// Getting Active Directory Identity's password
	domainJoinUserPassword = utils.GetPassword("Please type Active Directory's user password that will domain join ANF's SMB server and press [ENTER]:")
	if domainJoinUserPassword == "" {
		utils.ConsoleOutput("an error ocurred, domainJoinUserPassword cannot be empty")
		exitCode = 1
		return
	}

	// Getting subscription ID from authentication file
	config, err := utils.ReadAzureBasicInfoJSON(os.Getenv("AZURE_AUTH_LOCATION"))
	if err != nil {
		utils.ConsoleOutput(fmt.Sprintf("an error ocurred getting non-sensitive info from AzureAuthFile: %v", err))
		exitCode = 1
		return
	}

	// Checking if subnet exists before any other operation starts
	subnetID := fmt.Sprintf("/subscriptions/%v/resourceGroups/%v/providers/Microsoft.Network/virtualNetworks/%v/subnets/%v",
		*config.SubscriptionID,
		vnetResourceGroupName,
		vnetName,
		subnetName,
	)

	utils.ConsoleOutput(fmt.Sprintf("Checking if subnet %v exists.", subnetID))

	_, err = sdkutils.GetResourceByID(cntx, subnetID, virtualNetworksApiVersion)
	if err != nil {
		if string(err.Error()) == "NotFound" {
			utils.ConsoleOutput(fmt.Sprintf("error: subnet %v not found: %v", subnetID, err))
		} else {
			utils.ConsoleOutput(fmt.Sprintf("error: an error ocurred trying to check if %v exists: %v", subnetID, err))
		}

		exitCode = 1
		return
	}

	// Azure NetApp Files Account creation
	utils.ConsoleOutput("Creating Azure NetApp Files account...")

	// Building Active Directory List - please note that only one AD configuration is permitted per subscription and region
	activeDirectories := []netapp.ActiveDirectory{
		netapp.ActiveDirectory{
			DNS:           &dnsList,
			Domain:        &adFQDN,
			Username:      &domainJoinUserName,
			Password:      &domainJoinUserPassword,
			SmbServerName: &smbServerNamePrefix,
		},
	}

	account, err := sdkutils.CreateANFAccount(cntx, location, resourceGroupName, anfAccountName, activeDirectories, sampleTags)
	if err != nil {
		utils.ConsoleOutput(fmt.Sprintf("an error ocurred while creating account: %v", err))
		exitCode = 1
		return
	}
	acccountID = *account.ID
	utils.ConsoleOutput(fmt.Sprintf("Account successfully created, resource id: %v", *account.ID))

	// Capacity pool creation
	utils.ConsoleOutput("Creating Capacity Pool...")
	capacityPool, err := sdkutils.CreateANFCapacityPool(
		cntx,
		location,
		resourceGroupName,
		*account.Name,
		capacityPoolName,
		serviceLevel,
		capacityPoolSizeBytes,
		sampleTags,
	)
	if err != nil {
		utils.ConsoleOutput(fmt.Sprintf("an error ocurred while creating capacity pool: %v", err))
		exitCode = 1
		return
	}
	capacityPoolID = *capacityPool.ID
	utils.ConsoleOutput(fmt.Sprintf("Capacity Pool successfully created, resource id: %v", capacityPoolID))

	// SMB volume creation
	utils.ConsoleOutput("Creating SMB Volume...")
	smbVolume, err := sdkutils.CreateANFVolume(
		cntx,
		location,
		resourceGroupName,
		*account.Name,
		capacityPoolName,
		smbVolumeName,
		serviceLevel,
		subnetID,
		"",
		protocolTypes,
		volumeSizeBytes,
		false,
		false,
		sampleTags,
		netapp.VolumePropertiesDataProtection{}, // This empty object is provided as nil since dataprotection is not scope of this sample
	)

	if err != nil {
		utils.ConsoleOutput(fmt.Sprintf("an error ocurred while creating SMB volume: %v", err))
		exitCode = 1
		return
	}

	smbVolumeID = *smbVolume.ID
	utils.ConsoleOutput(fmt.Sprintf("SMB volume successfully created, resource id: %v", smbVolumeID))

	mountTargets := *smbVolume.MountTargets
	//*smbVolume.MountTargets[0].SmbServerFqdn
	utils.ConsoleOutput(fmt.Sprintf("\t====> SMB Server FQDN: %v", *mountTargets[0].SmbServerFqdn))
}

func exit(cntx context.Context) {
	utils.ConsoleOutput("Exiting")

	if shouldCleanUp {
		utils.ConsoleOutput("\tPerforming clean up")

		// SMB Volumes Cleanup
		utils.ConsoleOutput("\tCleaning up SMB volume...")
		err := sdkutils.DeleteANFVolume(
			cntx,
			resourceGroupName,
			anfAccountName,
			capacityPoolName,
			smbVolumeName,
		)
		if err != nil {
			utils.ConsoleOutput(fmt.Sprintf("an error ocurred while deleting volume: %v", err))
			exitCode = 1
			return
		}
		sdkutils.WaitForNoANFResource(cntx, smbVolumeID, 60, 60, false)
		utils.ConsoleOutput("\tVolume successfully deleted")

		// Pool Cleanup
		utils.ConsoleOutput("\tCleaning up capacity pool...")
		err = sdkutils.DeleteANFCapacityPool(
			cntx,
			resourceGroupName,
			anfAccountName,
			capacityPoolName,
		)
		if err != nil {
			utils.ConsoleOutput(fmt.Sprintf("an error ocurred while deleting capacity pool: %v", err))
			exitCode = 1
			return
		}
		sdkutils.WaitForNoANFResource(cntx, capacityPoolID, 60, 60, false)
		utils.ConsoleOutput("\tCapacity pool successfully deleted")

		// Account Cleanup
		utils.ConsoleOutput("\tCleaning up account...")
		err = sdkutils.DeleteANFAccount(
			cntx,
			resourceGroupName,
			anfAccountName,
		)
		if err != nil {
			utils.ConsoleOutput(fmt.Sprintf("an error ocurred while deleting account: %v", err))
			exitCode = 1
			return
		}
		utils.ConsoleOutput("\tAccount successfully deleted")
		utils.ConsoleOutput("\tCleanup completed!")
	}
}
