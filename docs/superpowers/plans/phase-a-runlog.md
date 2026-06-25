# Phase A Runlog — ROSA HCP manual provision (source for Phase B templates)

## Identities
- AWS IAM user: nebari-rosa-admin (AdministratorAccess) — root NOT usable with ROSA
- OCM org: Brandon Geraci (13601444), user `geraci`

## Account roles (HCP, mode auto)
- ManagedOpenShift-HCP-ROSA-Installer-Role
- ManagedOpenShift-HCP-ROSA-Support-Role
- ManagedOpenShift-HCP-ROSA-Worker-Role

## OIDC config
- id: 2r5fvmi16aph2cohrr3c2fo5lkpi3nnr (managed)

## Network (rosa create network rosa-quickstart-default-vpc, single AZ)
- stack/VPC name: nebari-ocp-poc
- VpcCidr: 10.0.0.0/16
- VPC: vpc-07fa0f5002dfa5e67
- private subnet: subnet-0242be239b6ec1d02
- public subnet:  subnet-02e0207719923d7b8
- NAT GW: nat-0e100ecf0baaca301

## Cluster create flags
- --hosted-cp --region us-east-1
- --compute-machine-type m5.xlarge --replicas 2
- --subnet-ids subnet-0242be239b6ec1d02,subnet-02e0207719923d7b8
- --oidc-config-id 2r5fvmi16aph2cohrr3c2fo5lkpi3nnr
