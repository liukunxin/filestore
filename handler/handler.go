package handler

import (
	"encoding/json"
	"fmt"
	"github.com/liukunxin/filestore/meta"
	"github.com/liukunxin/filestore/util"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"time"
	dblayer "github.com/liukunxin/filestore/db"
)

//UploadHandler：处理文件上传
func UploadHandler(w http.ResponseWriter, r *http.Request)  {
	if r.Method == "GET" {
		//返回上传html页面
		data,err := ioutil.ReadFile("./static/view/index.html")
		if err != nil{
			io.WriteString(w, "internel server error")
			return
		}
		io.WriteString(w, string(data))
	}else if r.Method == "POST" {
		//接受文件流及存储到本地目录
		file,head,err := r.FormFile("file")
		if err != nil {
			fmt.Printf("Failed to get data,err:%s\n", err.Error())
			return
		}
		defer file.Close()

		fileMeta := meta.FileMeta{
			FileName:head.Filename,
			Location:"E:\\tmp\\"+head.Filename,
			UploadAt:time.Now().Format("2006-01-02 15:04:05"),
		}
		newFile, err := os.Create(fileMeta.Location)
		if err != nil {
			fmt.Printf("Failed to create file:%s\n", err.Error())
			return
		}
		defer newFile.Close()

		fileMeta.FileSize,err = io.Copy(newFile, file)
		if err != nil {
			fmt.Printf("Failed to save data into file:%s\n", err.Error())
			return
		}

		newFile.Seek(0,0)
		fileMeta.FileSha1 = util.FileSha1(newFile)

		//TODO: 处理异常情况，比如跳转到一个上传失败页面
		_ = meta.UpdateFileMetaDB(fileMeta)

		r.ParseForm()
		username := r.Form.Get("username")
		suc := dblayer.OnUserFileUploadFinished(username, fileMeta.FileSha1,
			fileMeta.FileName, fileMeta.FileSize)
		if suc {
			http.Redirect(w, r, "/static/view/home.html", http.StatusFound)
		}else {
			w.Write([]byte("Upload Failed."))
		}
	}
}

//UploadSucHandler：上传已完成
func UploadSucHandler(w http.ResponseWriter, r *http.Request)  {
	io.WriteString(w, "Upload finished")
}

//GetFileMetaHandler：获取文件元信息
func GetFileMetaHandler(w http.ResponseWriter, r *http.Request)  {
	r.ParseForm()

	filehash := r.Form["filehash"][0]
	fMeta,err := meta.GetFileMetaDB(filehash)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	data,err := json.Marshal(fMeta)
	if err!=nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

// FileQueryHandler : 查询批量的文件元信息
func FileQueryHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	limitCnt, _ := strconv.Atoi(r.Form.Get("limit"))
	username := r.Form.Get("username")
	userFiles, err := dblayer.QueryUserFileMetas(username, limitCnt)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	data, err := json.Marshal(userFiles)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

// FileMetaUpdateHandler ： 更新元信息接口(重命名)
func FileMetaUpdateHandler(w http.ResponseWriter, r *http.Request){
	r.ParseForm()
	opType := r.Form.Get("op")
	fileSha1 := r.Form.Get("filehash")
	username := r.Form.Get("username")
	newFileName := r.Form.Get(fileSha1)

	if opType != "0" || len(newFileName) < 1 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// 更新用户文件表tbl_user_file中的文件名，tbl_file的文件名不用修改
	_ = dblayer.RenameFileName(username, fileSha1, newFileName)

	//返回最新的文件信息
	userFile, err := dblayer.QueryUserFileMeta(username, fileSha1)
	if err != nil {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	data,err := json.Marshal(userFile)
	if err!=nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

//FileDeleteHandler：删除文件及元信息
func FileDeleteHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	username := r.Form.Get("username")
	fileSha1 := r.Form.Get("filehash")

	//删除本地文件
	fMeta,err := meta.GetFileMetaDB(fileSha1)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	os.Remove(fMeta.Location)

	//删除文件表中的一条记录
	suc := dblayer.DeleteUserFile(username, fileSha1)
	if !suc {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// TryFastUploadHandler : 尝试秒传接口
func TryFastUploadHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	// 1. 解析请求参数
	username := r.Form.Get("username")
	filehash := r.Form.Get("filehash")
	filename := r.Form.Get("filename")
	filesize, _ := strconv.Atoi(r.Form.Get("filesize"))

	// 2. 从文件表中查询相同hash的文件记录
	fileMeta, err := meta.GetFileMetaDB(filehash)
	if err != nil {
		fmt.Println(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// 3. 查不到记录则返回秒传失败
	if fileMeta.FileSha1 == "" {
		resp := util.RespMsg{
			Code: -1,
			Msg:  "秒传失败，请访问普通上传接口",
		}
		w.Write(resp.JSONBytes())
		return
	}

	// 4. 上传过则将文件信息写入用户文件表， 返回成功
	suc := dblayer.OnUserFileUploadFinished(username, filehash, filename, int64(filesize))
	if suc {
		resp := util.RespMsg{
			Code: 0,
			Msg: "秒传成功",
		}
		w.Write(resp.JSONBytes())
		return
	}
	resp := util.RespMsg{
		Code: -2,
		Msg: "秒传失败，请稍后重试",
	}
	w.Write(resp.JSONBytes())
	return
}