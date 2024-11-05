/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
# 分区故障检测

启动时，先拉取所有 `DeployRegion`、`Node`、`Pod`，构建映射关系
```TEXT
root
├--region1
|   ├--Node1.1
|   |   ├--Pod1.1.1
|   |   └--Pod1.1.2
|   └--Node1.2
|       └--Pod1.2.1
└--region2
    ├--Node2.1
    |   └--Pod2.1.1
    └--Node2.2
        └--Pod2.2.1
```

每个节点，记录下Node的可分配资源，以及其上的Pod列表，每个Pod所需的资源，最终计算出Node的已分配资源
每个分区，记录分区下所有Node，计算总资源，以及已分配资源


监听`DeployRegion`改变事件：  （原则上DeployRegion在部署后，不应该再修改）如果分区存在，
监听`Node`改变事件：
监听`Pod`改变事件：
