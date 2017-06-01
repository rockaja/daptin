package server

import (
  "github.com/artpar/api2go"
  "gopkg.in/gin-gonic/gin.v1"
  log "github.com/Sirupsen/logrus"
  "github.com/artpar/gocms/server/resource"
  "io/ioutil"
  "encoding/json"
  "github.com/gorilla/context"
  "github.com/artpar/gocms/server/auth"
  "net/http"
  "errors"
  "fmt"
)

func CreateActionEventHandler(initConfig *CmsConfig, cruds map[string]*resource.DbResource) func(*gin.Context) {
  return func(c *gin.Context) {

    onEntity := c.Param("actionName")

    bytes, err := ioutil.ReadAll(c.Request.Body)
    if err != nil {
      c.Error(err)
      return
    }

    actionRequest := resource.ActionRequest{}
    json.Unmarshal(bytes, &actionRequest)
    log.Infof("Request body: %v", actionRequest)

    userReferenceId := context.Get(c.Request, "user_id").(string)
    userGroupReferenceIds := context.Get(c.Request, "usergroup_id").([]auth.GroupPermission)

    req := api2go.Request{
      PlainRequest: &http.Request{
        Method: "GET",
      },
    }

    referencedObject, err := cruds[actionRequest.Type].FindOne(actionRequest.Attributes[actionRequest.Type + "_id"].(string), req)

    if err != nil {
      c.Error(err)
      return
    }

    goModel := referencedObject.Result().(*api2go.Api2GoModel)
    obj := goModel.Data
    obj["__type"] = goModel.GetName()
    permission := cruds[actionRequest.Type].GetRowPermission(obj)

    if !permission.CanExecute(userReferenceId, userGroupReferenceIds) {
      c.AbortWithError(403, errors.New("Forbidden"))
    }

    if !cruds["world"].IsUserActionAllowed(userReferenceId, userGroupReferenceIds, actionRequest.Type, actionRequest.Action) {
      c.AbortWithError(403, errors.New("Forbidden"))
    }

    log.Infof("Handle event for [%v]", onEntity)

    action := cruds["action"].GetActionByName(actionRequest.Type, actionRequest.Action)

    inFieldMap, err := GetValidatedInFields(actionRequest, action, obj)

    if err != nil {
      c.AbortWithError(400, err)
      return
    }

    var res api2go.Responder

    for _, outcome := range action.OutFields {

      req, model := BuildOutcome(action.OnType, inFieldMap, outcome)
      context.Set(model.PlainRequest, "user_id", context.Get(c.Request, "user_id"))
      context.Set(model.PlainRequest, "user_id_integer", context.Get(c.Request, "user_id_integer"))
      context.Set(model.PlainRequest, "usergroup_id", context.Get(c.Request, "usergroup_id"))

      switch outcome.Method {
      case "POST":
        res, err = cruds[outcome.Type].Create(req, model)
        break
      case "UPDATE":
        res, err = cruds[outcome.Type].Update(req, model)
        break
      case "DELETE":
        res, err = cruds[outcome.Type].Delete(req.Data["reference_id"].(string), model)
        break
      case "EXECUTE":
        //res, err = cruds[outcome.Type].Create(req, model)
        break

      default:
        log.Errorf("Invalid outcome method: %v", outcome.Method)
        c.AbortWithError(500, errors.New("Invalid outcome"))
        return
      }

    }

    if err != nil {
      c.AbortWithError(400, err)
      return
    }

    c.JSON(200, res)

  }
}

func BuildOutcome(onType string, inFieldMap map[string]interface{}, outcome resource.Outcome) (*api2go.Api2GoModel, api2go.Request) {

  data := make(map[string]interface{})

  for key, field := range outcome.Attributes {
    data[key] = inFieldMap[field]
  }

  data[onType + "_id"] = inFieldMap[onType + "_id"]

  model := api2go.NewApi2GoModelWithData(outcome.Type, nil, 644, nil, data)

  req := api2go.Request{
    PlainRequest: &http.Request{
      Method: "POST",
    },
  }

  return model, req

}

func GetValidatedInFields(actionRequest resource.ActionRequest, action resource.Action, referencedObject map[string]interface{}) (map[string]interface{}, error) {

  dataMap := actionRequest.Attributes
  finalDataMap := make(map[string]interface{})
  for _, inField := range action.InFields {
    val, ok := dataMap[inField.ColumnName]
    if ok {
      finalDataMap[inField.ColumnName] = val
    } else if inField.DefaultValue != "" {

    } else {
      return nil, errors.New(fmt.Sprintf("Field %s cannot be blank", inField.Name))
    }
  }

  finalDataMap[actionRequest.Type + "_id"] = referencedObject["reference_id"]
  return finalDataMap, nil
}