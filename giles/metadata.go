package main

import (
	"encoding/json"
	"errors"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"strconv"
	"sync"
	"sync/atomic"
)

type RDBStreamId struct {
	StreamId uint32
	UUID     string
}

type Store struct {
	session      *mgo.Session
	db           *mgo.Database
	streams      *mgo.Collection
	metadata     *mgo.Collection
	pathmetadata *mgo.Collection
	apikeys      *mgo.Collection
	maxsid       *uint32
	streamlock   sync.Mutex
}

func NewStore(ip string, port int) *Store {
	log.Notice("Connecting to MongoDB at %v:%v...", ip, port)
	address := ip + ":" + strconv.Itoa(port)
	session, err := mgo.Dial(address)
	if err != nil {
		log.Critical("Could not connect to MongoDB: %v", err)
		return nil
	}
	log.Notice("...connected!")
	//session.SetMode(mgo.Eventual, true)
	db := session.DB("archiver")
	streams := db.C("streams")
	metadata := db.C("metadata")
	pathmetadata := db.C("pathmetadata")
	apikeys := db.C("apikeys")
	// create indexes
	index := mgo.Index{
		Key:        []string{"uuid"},
		Unique:     true,
		DropDups:   false,
		Background: true,
		Sparse:     true,
	}
	err = metadata.EnsureIndex(index)
	if err != nil {
		log.Fatal("Could not create index on metadata.uuid")
	}

	err = streams.EnsureIndex(index)
	if err != nil {
		log.Fatal("Could not create index on streams.uuid")
	}

	index.Key = []string{"Path"}
	err = pathmetadata.EnsureIndex(index)
	if err != nil {
		log.Fatal("Could not create index on pathmetadata.Path")
	}

	maxstreamid := &RDBStreamId{}
	streams.Find(bson.M{}).Sort("-streamid").One(&maxstreamid)
	var maxsid uint32 = 1
	if maxstreamid != nil {
		maxsid = maxstreamid.StreamId + 1
	}
	return &Store{session: session, db: db, streams: streams, metadata: metadata, pathmetadata: pathmetadata, apikeys: apikeys, maxsid: &maxsid}
}

func (s *Store) GetStreamId(uuid string) uint32 {
	s.streamlock.Lock()
	defer s.streamlock.Unlock()
	if v, found := UUIDCache[uuid]; found {
		return v
	}
	streamid := &RDBStreamId{}
	err := s.streams.Find(bson.M{"uuid": uuid}).One(&streamid)
	if err != nil {
		// not found, so create
		streamid.StreamId = (*s.maxsid)
		streamid.UUID = uuid
		inserterr := s.streams.Insert(streamid)
		if inserterr != nil {
			log.Error("Error inserting streamid", inserterr)
			return 0
		}
		atomic.AddUint32(s.maxsid, 1)
		log.Notice("Creating StreamId %v for uuid %v", streamid.StreamId, uuid)
		UUIDCache[uuid] = streamid.StreamId
	} else {
		UUIDCache[uuid] = streamid.StreamId
	}
	return streamid.StreamId
}

func (s *Store) CheckKey(apikey string, messages map[string]*SmapMessage) (bool, error) {
	var public bool
	query := s.apikeys.Find(bson.M{"key": apikey})
	count, err := query.Count()
	if err != nil {
		return false, err
	}
	if count > 1 {
		return false, errors.New("More than 1 API key with that value")
	}
	if count < 1 {
		return false, errors.New("No API key with that value")
	}
	query.Select(bson.M{"public": 1}).One(&public)
	//TODO: finish!
	// check the apikeys for the uuids of the messages that are being written. We need to be
	// OK to write to all of those
	return false, nil
}

/**
 * We use a pointer to the map so that we can edit it in-place
**/
func (s *Store) SavePathMetadata(messages *map[string]*SmapMessage) {
	/**
	 * We add the root metadata to everything in Contents
	**/
	if (*messages)["/"] != nil && (*messages)["/"].Metadata != nil {
		for _, path := range (*messages)["/"].Contents {
			_, err := s.pathmetadata.Upsert(bson.M{"Path": "/" + path}, bson.M{"$set": (*messages)["/"].Metadata})
			if err != nil {
				log.Error("Error saving metadata for", "/"+path)
				log.Panic(err)
			}
		}
		delete((*messages), "/")
	}
	/**
	 * For the rest of the keys, check if Contents is nonempty. If it is, we iterate through and update
	 * the metadata for that path
	**/
	for path, msg := range *messages {
		if msg.Metadata == nil {
			continue
		}
		if len(msg.Contents) > 0 {
			_, err := s.pathmetadata.Upsert(bson.M{"Path": path}, bson.M{"$set": msg.Metadata})
			if err != nil {
				log.Error("Error saving metadata for", path)
				log.Panic(err)
			}
			delete((*messages), path)
		}
	}

}

func (s *Store) SaveMetadata(msg *SmapMessage) {
	/* check if we have any metadata or properties.
	   This should get hit once per stream unless the stream's
	   metadata changes
	*/
	var prefixMetadata bson.M
	//TODO: this takes up a lot of memory because we run it on every write. How can we know to skip it?
	for _, prefix := range getPrefixes(msg.Path) {
		s.pathmetadata.Find(bson.M{"Path": prefix}).Select(bson.M{"_id": 0, "Path": 0}).One(&prefixMetadata)
		for k, v := range prefixMetadata {
			_, err := s.metadata.Upsert(bson.M{"uuid": msg.UUID}, bson.M{"$set": bson.M{"Metadata." + k: v}})
			if err != nil {
				log.Error("Error saving metadata for", msg.UUID)
				log.Panic(err)
			}
		}
	}
	if msg.Metadata == nil && msg.Properties == nil && msg.Actuator == nil {
		return
	}
	if msg.Path != "" {
		_, err := s.metadata.Upsert(bson.M{"uuid": msg.UUID}, bson.M{"$set": bson.M{"Path": msg.Path}})
		if err != nil {
			log.Error("Error saving path for", msg.UUID)
			log.Panic(err)
		}
	}
	if msg.Metadata != nil {
		for k, v := range msg.Metadata {
			_, err := s.metadata.Upsert(bson.M{"uuid": msg.UUID}, bson.M{"$set": bson.M{"Metadata." + k: v}})
			if err != nil {
				log.Error("Error saving metadata for", msg.UUID)
				log.Panic(err)
			}
		}
	}
	if msg.Properties != nil {
		for k, v := range msg.Properties {
			_, err := s.metadata.Upsert(bson.M{"uuid": msg.UUID}, bson.M{"$set": bson.M{"Properties." + k: v}})
			if err != nil {
				log.Error("Error saving properties for", msg.UUID)
				log.Panic(err)
			}
		}
	}
	if msg.Actuator != nil {
		_, err := s.metadata.Upsert(bson.M{"uuid": msg.UUID}, bson.M{"$set": bson.M{"Actuator": msg.Actuator}})
		if err != nil {
			log.Error("Error saving actuator for", msg.UUID)
			log.Panic(err)
		}
	}
}

func (s *Store) Query(stringquery []byte, apikey string) ([]byte, error) {
	if apikey != "" {
		log.Info("query with key:", apikey)
	}
	log.Info(string(stringquery))
	var res []bson.M
	var d []byte
	var err error
	ast := parse(string(stringquery))
	where := ast.Where.ToBson()
	switch ast.TargetType {
	case TAGS_TARGET:
		var staged *mgo.Query
		target := ast.Target.(*TagsTarget).ToBson()
		if len(target) == 0 {
			staged = s.metadata.Find(where).Select(bson.M{"_id": 0})
		} else {
			target["_id"] = 0
			staged = s.metadata.Find(where).Select(target)
		}
		if ast.Target.(*TagsTarget).Distinct {
			var res2 []interface{}
			err = staged.Distinct(ast.Target.(*TagsTarget).Contents[0], &res2)
			d, err = json.Marshal(res2)
		} else {
			err = staged.All(&res)
			d, err = json.Marshal(res)
		}
	case SET_TARGET:
		target := ast.Target.(*SetTarget).Updates
		info, err2 := s.metadata.UpdateAll(where, bson.M{"$set": target})
		if err2 != nil {
			return d, err2
		}
		log.Info("Updated", info.Updated, "records")
		d, err = json.Marshal(bson.M{"Updated": info.Updated})
	case DATA_TARGET:
		target := ast.Target.(*DataTarget)
		uuids := store.GetUUIDs(ast.Where.ToBson())
		if target.Streamlimit > -1 {
			uuids = uuids[:target.Streamlimit] // limit number of streams
		}
		var response []SmapResponse
		switch target.Type {
		case IN:
			start := uint64(target.Start.Unix())
			end := uint64(target.End.Unix())
			log.Debug("start", start, "end", end)
			response, err = tsdb.GetData(uuids, start, end)
		case AFTER:
			ref := uint64(target.Ref.Unix())
			log.Debug("after", ref)
			response, err = tsdb.Next(uuids, ref, target.Limit)
		case BEFORE:
			ref := uint64(target.Ref.Unix())
			log.Debug("before", ref)
			response, err = tsdb.Prev(uuids, ref, target.Limit)
		}
		if err != nil {
			return d, err
		}
		d, err = json.Marshal(response)
	}
	return d, err
}

/*
   Return all metadata for a certain UUID
*/
func (s *Store) TagsUUID(uuid string) ([]byte, error) {
	var d []byte
	staged := s.metadata.Find(bson.M{"uuid": uuid}).Select(bson.M{"_id": 0})
	res := []bson.M{}
	err := staged.All(&res)
	if err != nil {
		return d, err
	}
	d, err = json.Marshal(res)
	return d, err
}

/*
  Resolve a query to a slice of UUIDs
*/
func (s *Store) GetUUIDs(where bson.M) []string {
	var tmp []bson.M
	err := s.metadata.Find(where).Select(bson.M{"uuid": 1}).All(&tmp)
	if err != nil {
		log.Panic(err)
	}
	var res = []string{}
	for _, uuid := range tmp {
		res = append(res, uuid["uuid"].(string))
	}
	return res
}

/*
  Resolve a query to a slice of StreamIds
*/
func (s *Store) GetStreamIds(where bson.M) []uint32 {
	var tmp []bson.M
	var res []uint32
	err := s.metadata.Find(where).Select(bson.M{"uuid": 1}).All(&tmp)
	if err != nil {
		log.Panic(err)
	}
	for _, uuid := range tmp {
		res = append(res, s.GetStreamId(uuid["uuid"].(string)))
	}
	return res
}
