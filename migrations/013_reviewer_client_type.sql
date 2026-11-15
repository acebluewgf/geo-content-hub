-- 013: 增加「审批监控」客户端类型
ALTER TABLE api_clients MODIFY type ENUM('content','publisher','reviewer','admin') NOT NULL;
