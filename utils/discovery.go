package utils

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/sjson"
)

// ReadTerraformerStateFile ..
// TF 0.12 compatible
func ReadTerraformerStateFile(terraformerStateFile string) ResourceList {
	var rList ResourceList
	tfData := TerraformSate{}

	tfFile, err := ioutil.ReadFile(terraformerStateFile)
	if err != nil {
		log.Fatal(err)
	}

	err = json.Unmarshal([]byte(tfFile), &tfData)
	if err != nil {
		log.Fatal(err)
	}

	for i := 0; i < len(tfData.Modules); i++ {
		rData := Resource{}
		for k := range tfData.Modules[i].Resources {
			rData.ResourceName = k
			rData.ResourceType = tfData.Modules[i].Resources[k].ResourceType
			for p := range tfData.Modules[i].Resources[k].Primary {
				if p == "attributes" {
					rData.ID = tfData.Modules[i].Resources[k].Primary[p].ID
				}
			}
			rList = append(rList, rData)
		}
	}

	log.Printf("Total (%d) resource in (%s).\n", len(rList), terraformerStateFile)
	return rList
}

// ReadTerraformStateFile ..
// TF 0.13+ compatible
func ReadTerraformStateFile(terraformStateFile, repoType string) map[string]interface{} {
	rIDs := make(map[string]interface{})
	tfData := TerraformSate{}

	tfFile, err := ioutil.ReadFile(terraformStateFile)
	if err != nil {
		log.Fatal(err)
	}

	err = json.Unmarshal([]byte(tfFile), &tfData)
	if err != nil {
		log.Fatal(err)
	}

	for i := 0; i < len(tfData.Resources); i++ {
		rData := Resource{}
		var key string
		rData.ResourceName = tfData.Resources[i].ResourceName
		rData.ResourceType = tfData.Resources[i].ResourceType
		for k := 0; k < len(tfData.Resources[i].Instances); k++ {
			rData.ID = tfData.Resources[i].Instances[k].Attributes.ID
			if tfData.Resources[i].Instances[k].DependsOn != nil {
				rData.DependsOn = tfData.Resources[i].Instances[k].DependsOn
			}

			if repoType == "discovery" {
				key = rData.ResourceType + "." + rData.ResourceName
			} else {
				key = rData.ResourceType + "." + rData.ID
			}
			rData.ResourceIndex = i
			rIDs[key] = rData
		}
	}

	log.Printf("Total (%d) resource in (%s).\n", len(rIDs), terraformStateFile)
	return rIDs
}

// DiscoveryImport ..
func DiscoveryImport(configName string, opts []string, randomID, discoveryDir string) error {
	log.Printf("# let's import the resources (%s) 2/6:\n", opts)

	// Import the terraform resources & state files.
	err := TerraformerImport(discoveryDir, opts, configName, &planTimeOut, randomID)
	if err != nil {
		return err
	}

	log.Println("# Writing HCL Done!")
	log.Println("# Writing TFState Done!")

	//Check terraform version compatible
	log.Println("# now, we can do some infra as code ! First, update the IBM Terraform provider to support TF 0.13 [3/6]:")
	err = UpdateProviderFile(discoveryDir, randomID, &planTimeOut)
	if err != nil {
		return err
	}

	//Run terraform init commnd
	log.Println("# we need to init our Terraform project [4/6]:")
	err = TerraformInit(discoveryDir, "", &planTimeOut, randomID)
	if err != nil {
		return err
	}

	//Run terraform refresh commnd on the generated state file
	log.Println("# and finally compare what we imported with what we currently have [5/6]:")
	err = TerraformRefresh(discoveryDir, "", &planTimeOut, randomID)
	if err != nil {
		return err
	}

	return nil
}

// UpdateProviderFile ..
func UpdateProviderFile(discoveryDir, randomID string, timeout *time.Duration) error {
	providerTF := discoveryDir + "/provider.tf"
	input, err := ioutil.ReadFile(providerTF)
	if err != nil {
		return err
	}

	lines := strings.Split(string(input), "\n")

	for i, line := range lines {
		if strings.Contains(line, "version") {
			lines[i] = "source = \"IBM-Cloud/ibm\""
		}
	}
	output := strings.Join(lines, "\n")
	err = ioutil.WriteFile(providerTF, []byte(output), 0644)
	if err != nil {
		return err
	}

	//Replace provider path in state file
	err = TerraformReplaceProvider(discoveryDir, randomID, &planTimeOut)
	if err != nil {
		return err
	}
	return nil
}

// MergeStateFile ..
func MergeStateFile(terraformfObj, terraformerObj map[string]interface{}, src, dest, configDir, scenario, randomID string, timeout *time.Duration) error {
	var resourceList []string

	//Read discovery state file
	content, err := ioutil.ReadFile(src)
	if err != nil {
		return err
	}
	statefilecontent := string(content)

	//Loop through each discovery repo resource with local repo resource
	for _, dValue := range terraformerObj {
		//Ignore local type resource from discovery repo
		if dValue.(Resource).ResourceName == "local" {
			continue
		}

		//Discovery resource
		discovery_resource := dValue.(Resource).ResourceType + "." + dValue.(Resource).ID

		//Check discovery resource exist in local repo.
		//If resource not exist, copy the discovery resource to resourceList for moving to local repo
		if terraformfObj[discovery_resource] == nil {
			discovery_resource := dValue.(Resource).ResourceType + "." + dValue.(Resource).ResourceName
			resourceList = append(resourceList, discovery_resource)
		} else {
			//Resource allready exist in local repo
			continue
		}

		//Check discovery resource has got depends_on attribute
		//If depends_on attribute exist , Get depends_on resource name from local repo(if exist) & update in discovery state file.
		if dValue.(Resource).DependsOn != nil {
			var dependsOn []string
			for i, d := range dValue.(Resource).DependsOn {
				parent_resource := terraformerObj[d].(Resource).ResourceType + "." + terraformerObj[d].(Resource).ID

				//Get parent resource from local repo
				if terraformfObj[parent_resource] != nil {
					//Get depends_on resource deails from local repo to update in discovery state file
					parent_resource = terraformfObj[parent_resource].(Resource).ResourceType + "." + terraformfObj[parent_resource].(Resource).ResourceName
					dependsOn = append(dependsOn, parent_resource)

					//Update depends_on parameter in discovery state file
					statefilecontent, err = sjson.Set(statefilecontent, "resources."+strconv.Itoa(dValue.(Resource).ResourceIndex)+".instances.0.dependencies."+strconv.Itoa(i), parent_resource)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	//Copy updated state file content back to disovery repo
	err = ioutil.WriteFile(src, []byte(statefilecontent), 0644)
	if err != nil {
		return err
	}

	// Move resource from discovery repo to local repo state file
	if len(resourceList) > 0 {
		for _, resource := range resourceList {
			err = run("terraform", []string{"state", "mv", fmt.Sprintf("-state=%s", src), fmt.Sprintf("-state-out=%s", dest), resource, resource}, configDir, scenario, timeout, randomID)
			if err != nil {
				return err
			}
		}
		log.Printf("\n# Discovery service successfuly moved '(%v)' resources from (%s) to (%s).", len(resourceList), src, dest)
	} else {
		log.Printf("\n# Discovery service didn't find any resource to move from (%s) to (%s).", src, dest)
	}

	return nil
}
