package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"github.com/zhezack/pgdeltastream/db"
	"github.com/zhezack/pgdeltastream/types"
)

var wsupgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func StartServer(host string, port int) {
	var err error
	session := &types.Session{}
	r := gin.Default()
	v1 := r.Group("/v1")
	{
		v1.GET("/init", func(c *gin.Context) {
			err = initDB(session)
			if err != nil {
				e := fmt.Sprintf("unable to init session")
				log.WithError(err).Error(e)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": e,
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"slotName": session.SlotName,
			})
		})

		snapshotRoute := v1.Group("/snapshot")
		{

			snapshotRoute.POST("/data", func(c *gin.Context) {
				if session.SnapshotName == "" {
					e := fmt.Sprintf("snapshot not available: call /init to initialize a new slot and snapshot")
					log.Error(e)
					c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
						"error": e,
					})
					return
				}

				// get data with table, offset, limits
				var postData types.SnapshotDataJSON
				//err = c.MustBindWith(&postData, binding.JSON)
				err = c.ShouldBindJSON(&postData)
				if err != nil {
					e := fmt.Sprintf("invalid input JSON")
					log.WithError(err).Error(e)
					c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
						"error": e,
					})
					return
				}

				err = validateSnapshotDataJSON(&postData)
				if err != nil {
					e := err.Error()
					log.WithError(err).Error(e)
					c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
						"error": e,
					})
					return
				}

				log.Infof("Snapshot data requested for table: %s, offset: %d, limit: %d", postData.Table, *postData.Offset, *postData.Limit)

				data, err := snapshotData(session, &postData)
				if err != nil {
					e := fmt.Sprintf("unable to get snapshot data")
					log.WithError(err).Error(e)
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
						"error": e,
					})
					return
				}

				c.JSON(http.StatusOK, data)
			})

			/*
				snapshotRoute.GET("/end", func(c *gin.Context) {
					// end snapshot
					c.String(200, "end") // TODO
				})
			*/
		}

		lrRoute := v1.Group("/lr")
		{
			lrRoute.GET("/stream", func(c *gin.Context) { // /stream?slotName=my_slot
				slotName := c.Query("slotName")
				if slotName == "" {
					e := fmt.Sprintf("no slotName provided")
					log.WithError(err).Error(e)
					c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
						"error": e,
					})
					return
				}

				log.Info("LR Stream requested for slot ", slotName)
				session.SlotName = slotName

				// now upgrade the HTTP  connection to a websocket connection
				wsConn, err := wsupgrader.Upgrade(c.Writer, c.Request, nil)
				if err != nil {
					e := fmt.Sprintf("could not upgrade to websocket connection")
					log.WithError(err).Error(e)
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
						"error": e,
					})
					return
				}
				session.WSConn = wsConn

				// begin streaming
				err = lrStream(session)
				if err != nil {
					e := fmt.Sprintf("could not create stream")
					log.WithError(err).Error(e)
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
						"error": e,
					})
					return
				}
			})
		}
	}

	r.Run(fmt.Sprintf("%s:%d", host, port))
}

// create replication slot ex and get snapshot name, consistent point
// return slotname
func initDB(session *types.Session) error {
	var err error
	// initilize the connections for the session
	resetSession(session)
	err = db.Init(session)
	if err != nil {
		return err
	}

	return nil
}

// validate fields in the snapshot data request JSON
func validateSnapshotDataJSON(requestData *types.SnapshotDataJSON) error {
	ob := requestData.OrderBy
	if ob != nil {
		if ob.Column == "" {
			return fmt.Errorf("required field 'column' missing in 'order_by'")
		}
		if !(strings.EqualFold(ob.Order, "asc") || strings.EqualFold(ob.Order, "desc")) {
			return fmt.Errorf("order_by order direction can only be either 'ASC' or 'DESC'")
		}
	}
	return nil
}

func snapshotData(session *types.Session, requestData *types.SnapshotDataJSON) ([]map[string]interface{}, error) {
	return db.SnapshotData(session, requestData)
}

func lrStream(session *types.Session) error {
	// reset the connections
	err := resetSession(session)
	if err != nil {
		log.WithError(err).Error("Could not create replication connection")
		return fmt.Errorf("Could not create replication connection")
	}

	wsErr := make(chan error, 1)
	go db.LRListenAck(session, wsErr) // concurrently listen on the ws for ack messages
	go db.LRStream(session)           // listen for WAL messages and send them over ws

	select {
	/*case <-c.Writer.CloseNotify(): // ws closed // ?this doesn't work?
	  log.Warn("Websocket connection closed. Cancelling context.")
	  cancelFunc()
	*/
	case <-wsErr: // ws closed
		log.Warn("Websocket connection closed. Cancelling context.")
		// cancel session context
		session.CancelFunc()
		// close connections
		err = session.WSConn.Close()
		if err != nil {
			log.WithError(err).Error("Could not close websocket connection")
		}

		err = session.ReplConn.Close()
		if err != nil {
			log.WithError(err).Error("Could not close replication connection")
		}

	}
	return nil
}

// Cancel the currently running session
// Recreate replication connection
func resetSession(session *types.Session) error {
	var err error
	// cancel the currently running session
	if session.CancelFunc != nil {
		session.CancelFunc()
	}

	// close websocket connection
	if session.WSConn != nil {
		//err = session.WSConn.Close()
		if err != nil {
			return err
		}
	}

	// create new context
	ctx, cancelFunc := context.WithCancel(context.Background())
	session.Ctx = ctx
	session.CancelFunc = cancelFunc

	// create the replication connection
	err = db.CheckAndCreateReplConn(session)
	if err != nil {
		return err
	}

	return nil

}
