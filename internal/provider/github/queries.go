package github

// GraphQL documents. Projects v2 has no REST API, so every board-side read
// and write goes through these.

const fieldValuesFragment = `
fragment FV on ProjectV2ItemFieldValueConnection {
  nodes {
    __typename
    ... on ProjectV2ItemFieldTextValue         { text   field { ... on ProjectV2FieldCommon { name } } }
    ... on ProjectV2ItemFieldDateValue         { date   field { ... on ProjectV2FieldCommon { name } } }
    ... on ProjectV2ItemFieldNumberValue       { number field { ... on ProjectV2FieldCommon { name } } }
    ... on ProjectV2ItemFieldSingleSelectValue { name   field { ... on ProjectV2FieldCommon { name } } }
  }
}`

const issueFragment = `
fragment IssueFields on Issue {
  __typename
  number title body url state createdAt updatedAt
  labels(first:30)    { nodes { name } }
  assignees(first:10) { nodes { login } }
}`

const listQuery = `
query($project:ID!, $cursor:String) {
  node(id:$project) {
    ... on ProjectV2 {
      items(first:100, after:$cursor) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          isArchived
          content { ... on Issue { ...IssueFields } }
          fieldValues(first:100) { ...FV }
        }
      }
    }
  }
}` + issueFragment + fieldValuesFragment

const getQuery = `
query($owner:String!, $repo:String!, $number:Int!) {
  repository(owner:$owner, name:$repo) {
    issue(number:$number) {
      id
      ...IssueFields
      projectItems(first:20) {
        nodes {
          id
          isArchived
          project { id }
          fieldValues(first:100) { ...FV }
        }
      }
    }
  }
}` + issueFragment + fieldValuesFragment

const addItemMutation = `
mutation($project:ID!, $content:ID!) {
  addProjectV2ItemById(input:{projectId:$project, contentId:$content}) { item { id } }
}`

const updateFieldMutation = `
mutation($project:ID!, $item:ID!, $field:ID!, $value:ProjectV2FieldValue!) {
  updateProjectV2ItemFieldValue(input:{projectId:$project, itemId:$item, fieldId:$field, value:$value}) {
    projectV2Item { id }
  }
}`

const clearFieldMutation = `
mutation($project:ID!, $item:ID!, $field:ID!) {
  clearProjectV2ItemFieldValue(input:{projectId:$project, itemId:$item, fieldId:$field}) {
    projectV2Item { id }
  }
}`

const archiveItemMutation = `
mutation($project:ID!, $item:ID!) {
  archiveProjectV2Item(input:{projectId:$project, itemId:$item}) { item { id } }
}`

const createProjectMutation = `
mutation($owner:ID!, $title:String!) {
  createProjectV2(input:{ownerId:$owner, title:$title}) { projectV2 { id number title url } }
}`

const ownerQuery = `
query($login:String!) { repositoryOwner(login:$login) { __typename id } }`

const repoQuery = `
query($owner:String!, $repo:String!) { repository(owner:$owner, name:$repo) { id nameWithOwner } }`

const linkRepoMutation = `
mutation($project:ID!, $repo:ID!) {
  linkProjectV2ToRepository(input:{projectId:$project, repositoryId:$repo}) { repository { nameWithOwner } }
}`

const createFieldMutation = `
mutation($project:ID!, $type:ProjectV2CustomFieldType!, $name:String!, $options:[ProjectV2SingleSelectFieldOptionInput!]) {
  createProjectV2Field(input:{projectId:$project, dataType:$type, name:$name, singleSelectOptions:$options}) {
    projectV2Field {
      ... on ProjectV2Field { id name dataType }
      ... on ProjectV2SingleSelectField { id name dataType options { id name } }
    }
  }
}`

const updateFieldOptionsMutation = `
mutation($field:ID!, $options:[ProjectV2SingleSelectFieldOptionInput!]!) {
  updateProjectV2Field(input:{fieldId:$field, singleSelectOptions:$options}) {
    projectV2Field { ... on ProjectV2SingleSelectField { id name options { id name } } }
  }
}`
