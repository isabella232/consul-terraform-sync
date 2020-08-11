# This file is generated by Consul NIA.
#
# The HCL blocks, arguments, variables, and values are derived from the
# operator configuration for Consul NIA. Any manual changes to this file
# may not be preserved and could be clobbered by a subsequent update.

terraform {
  required_version = "~>0.13.0"
  required_providers {
    testProvider = {
      source  = "namespace/testProvider"
      version = "1.0.0"
    }
  }
  backend "consul" {
    path   = "consul-nia/terraform"
    scheme = "https"
  }
}

provider "testProvider" {
  attr  = var.testProvider.attr
  count = var.testProvider.count
}

# user description for task named 'test'
module "test" {
  source   = "namespace/consul-nia/consul//modules/test"
  version  = "0.0.0"
  services = var.services

  bool_true = var.bool_true
  one       = var.one
}