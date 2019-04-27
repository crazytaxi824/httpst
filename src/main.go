package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	TotalRequest        int     // 总请求数量
	FailedTransactions  int     // 请求失败的次数
	SuccessRate         float64 // 成功率
	TransactionRate     float64 // 平均每秒处理请求数 = 总请求次数/总耗时
	ElapsedTime         float64 // 总耗时
	LongestTransaction  float64 // 最长耗时
	ShortestTransaction float64 // 最短耗时

	waitReq   sync.WaitGroup // 等待请求完成
	waitStats sync.WaitGroup // 等待监听服务完成
	showData  bool           // 是否显示返回数据
)

// 通道组
type Signal struct {
	MaxMinValue        chan float64
	FailedTransactions chan bool
	TotalRequest       chan bool
}

// 请求数据
type RequestContext struct {
	RawUrl        string
	Method        string
	Body          string
	HeaderKVSlice [][2]string
}

func main() {
	users := flag.Uint("c", 1, "并发次数")
	urlReq := flag.String("r", "", "请求路径 eg:http://www.baidu.com")
	header := flag.String("H", "", "header, eg: token:a.b.c&Content-Type:application/x-www-form-urlencoded")
	method := flag.String("m", "get", "请求方法：get/post...")
	body := flag.String("P", "", "param, eg: id=1&name=abc")
	sd := flag.Bool("s", false, "是否显示返回数据 (default false)")
	flag.Parse()

	// 测试用 ------------------
	//*header = "x-user-token:eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJvbkRUWDVYdzRmaU9OYUExQTBKNUZiRGNCeXo0IiwidW5pcXVlTmFtZSI6Im9uRFRYNVh3NGZpT05hQTFBMEo1RmJEY0J5ejQiLCJpZCI6IjExMTg4MTIxMjQ2MDk1MTU1MjAiLCJuYW1lIjoiTVJIIiwiZXhwIjoxNTU2MTA3NzkxfQ.XsmiYyS1ualsT6QyTdR6uSUPK5A3DbgLz9DJFLQKt6CMCmlw1pIr_uVLSs0IscdftvQK6l7Pw1PlTTuWiYVVzRrNROlvrtfdPtn_4l1JUVZMSFBOyx2uE7_BlHzCmStGrRG_x2PgG8rHbbpO-XXphVhN6Ln3lPnZhi5aARIWxAY"
	//*body = "id=1&name=lq"
	//*method = "post"
	//*users = 10
	//*sd = true

	//*urlReq = "127.0.0.1:18080/post/123"
	//*urlReq = "www.baidu.com"
	//*urlReq = "http://test.koudejun.com/api/1/v1/item/u/item/list?page=1&pageSize=6"
	//*urlReq = "http://127.0.0.1:18080/test"
	//*urlReq := "http://192.168.2.250:20004/item/u/item/list?page=1&pageSize=6"
	//*urlReq := "http://192.168.2.116:20086/item/u/item/list?page=1&pageSize=6"
	//-------------------

	// 判断并发次数
	if *users < uint(1) {
		fmt.Println("并发次数不能小于1")
		return
	}

	showData = *sd

	// 接受数据
	var reqData RequestContext

	// 获取 rawUrl
	reqData.RawUrl = strings.TrimSpace(*urlReq)
	if reqData.RawUrl == "" {
		fmt.Println("请求路径不能为空")
		return
	}
	if len(reqData.RawUrl) < 8 {
		reqData.RawUrl = "http://" + reqData.RawUrl
	} else if reqData.RawUrl[:7] != "http://" && reqData.RawUrl[:8] != "https://" {
		reqData.RawUrl = "http://" + reqData.RawUrl
	}

	// 获取 header
	if *header != "" {
		headerSlice := strings.Split(*header, "&")

		for _, v := range headerSlice {
			kv := strings.SplitN(v, ":", 2)
			if len(kv) != 2 {
				fmt.Println("header格式错误")
				return
			}

			var kvArray [2]string
			kvArray[0] = kv[0]
			kvArray[1] = kv[1]
			reqData.HeaderKVSlice = append(reqData.HeaderKVSlice, kvArray)
		}

	} else {
		reqData.HeaderKVSlice = nil
	}

	// 获取body
	reqData.Body = strings.TrimSpace(*body)

	reqData.Method = strings.ToUpper(strings.TrimSpace(*method))

	// -----------------
	var signal Signal
	signal.MaxMinValue = make(chan float64)
	signal.FailedTransactions = make(chan bool)
	signal.TotalRequest = make(chan bool)

	waitStats.Add(3)
	go replaceMaxMinValue(signal.MaxMinValue)
	go failedTransactionCount(signal.FailedTransactions)
	go totalRequestCount(signal.TotalRequest)

	startTime := time.Now()

	var i uint = 0
	for i < *users {
		waitReq.Add(1)
		// 发送请求
		go httpSendRequest(&reqData, signal)
		i++
	}

	waitReq.Wait()

	// 统计总耗时
	endTime := time.Now()

	// 关闭chan
	close(signal.MaxMinValue)
	close(signal.FailedTransactions)
	close(signal.TotalRequest)

	// 等待监听服务退出
	waitStats.Wait()

	// 总耗时
	ElapsedTime = endTime.Sub(startTime).Seconds()
	// 成功请求数量
	SuccessTransactions := TotalRequest - FailedTransactions
	// 成功率
	SuccessRate = float64(SuccessTransactions) / float64(TotalRequest) * 100

	// 每秒处理次数
	TransactionRate = float64(TotalRequest) / ElapsedTime

	fmt.Println()
	fmt.Println("总请求数量:                     ", TotalRequest, "次")
	fmt.Println("成功请求数量: 	                ", SuccessTransactions, "次")

	if FailedTransactions > 0 {
		fmt.Printf("%c[1;40;31m%s%c[0m\n", 0x1B, "失败请求数量: 	                 "+fmt.Sprintf("%d", FailedTransactions)+"次", 0x1B)
	} else {
		fmt.Println("失败请求数量: 	                ", FailedTransactions, "次")
	}

	if SuccessRate < 100 {
		fmt.Printf("%c[1;40;31m%s%c[0m\n", 0x1B, "请求成功率:                      "+fmt.Sprintf("%.2f", SuccessRate)+" %", 0x1B)
	} else {
		fmt.Printf("%c[1;40;33m%s%c[0m\n", 0x1B, "请求成功率:                      "+fmt.Sprintf("%.2f", SuccessRate)+" %", 0x1B)
	}

	fmt.Println("平均每秒处理请求数:             ", fmt.Sprintf("%.2f", TransactionRate)+" 次/秒")
	fmt.Println("最长耗时:     	                ", fmt.Sprintf("%.3f", LongestTransaction)+" 秒")
	fmt.Println("最短耗时:     	                ", fmt.Sprintf("%.3f", ShortestTransaction)+" 秒")
	fmt.Println("总耗时:       	                ", fmt.Sprintf("%.3f", ElapsedTime)+" 秒")
	fmt.Println()

}

func httpSendRequest(reqData *RequestContext, signal Signal) {
	defer waitReq.Done()

	// 准备请求
	client := http.Client{}
	req, err := http.NewRequest(reqData.Method, reqData.RawUrl, strings.NewReader(reqData.Body))
	if err != nil {
		fmt.Printf("%c[1;40;33m%s%c[0m\n", 0x1B, "Request ERROR: "+err.Error(), 0x1B)
		return
	}

	if reqData.Method != "GET" {
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded;param=value")
	}

	// header
	for _, v := range reqData.HeaderKVSlice {
		req.Header.Add(v[0], v[1])
	}

	// 开始计时
	startTime := time.Now()

	// 开始请求
	resp, err := client.Do(req)

	// 统计总请求数量
	signal.TotalRequest <- true
	if err != nil {
		// 统计请求失败数量
		signal.FailedTransactions <- true

		fmt.Printf("%c[1;40;31m%s%c[0m\n", 0x1B, "ERROR: "+err.Error(), 0x1B)
		return
	}
	defer resp.Body.Close()

	// 结束计时
	endTime := time.Now()

	// 单个耗时
	diffTime := endTime.Sub(startTime).Seconds()

	// 是否显示返回数据
	if showData {
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Println(err)
			return
		}

		fmt.Println("response data: ", string(b))
	}

	// 打印结果
	if req.URL.RawQuery != "" {
		if resp.StatusCode >= 200 && resp.StatusCode < 210 {
			fmt.Println(req.Proto, "| code:", fmt.Sprintf("%c[0;40;33m%s%c[0m", 0x1B, fmt.Sprintf("%d", resp.StatusCode), 0x1B), "|", fmt.Sprintf("%.3f", diffTime)+" 秒", "==>", req.Method, req.Host+req.URL.Path+"?"+req.URL.RawQuery)
		} else {
			fmt.Println(req.Proto, "| code:", fmt.Sprintf("%c[1;40;31m%s%c[0m", 0x1B, fmt.Sprintf("%d", resp.StatusCode), 0x1B), "|", fmt.Sprintf("%.3f", diffTime)+" 秒", "==>", req.Method, req.Host+req.URL.Path+"?"+req.URL.RawQuery)
		}
	} else {
		if resp.StatusCode >= 200 && resp.StatusCode < 210 {
			fmt.Println(req.Proto, "| code:", fmt.Sprintf("%c[0;40;33m%s%c[0m", 0x1B, fmt.Sprintf("%d", resp.StatusCode), 0x1B), "|", fmt.Sprintf("%.3f", diffTime)+" 秒", "==>", req.Method, req.Host+req.URL.Path)
		} else {
			fmt.Println(req.Proto, "| code:", fmt.Sprintf("%c[1;40;31m%s%c[0m", 0x1B, fmt.Sprintf("%d", resp.StatusCode), 0x1B), "|", fmt.Sprintf("%.3f", diffTime)+" 秒", "==>", req.Method, req.Host+req.URL.Path)
		}
	}

	// 统计最长最短时间
	signal.MaxMinValue <- diffTime

	return
}

// 替换最大最小值
func replaceMaxMinValue(ch <-chan float64) {
	defer waitStats.Done()

	for {
		select {
		case v, ok := <-ch:
			if !ok {
				return
			}

			if v > LongestTransaction {
				LongestTransaction = v
			}

			if ShortestTransaction == 0 {
				ShortestTransaction = v
			} else if v < ShortestTransaction {
				ShortestTransaction = v
			}
		}
	}
}

func failedTransactionCount(ch <-chan bool) {
	defer waitStats.Done()

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}

			FailedTransactions += 1
		}
	}
}

func totalRequestCount(ch <-chan bool) {
	defer waitStats.Done()

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}

			TotalRequest += 1
		}
	}
}
