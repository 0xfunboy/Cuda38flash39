#pragma once
#include <functional>
#include <optional>
#include <string>
#include <vector>
struct Job{int id;std::string payload;};
class Queue {
    enum class State{pending,inflight,done};
    struct Item{Job job;State state;};
    std::vector<Item> items_;
public:
    void add(Job job);
    std::optional<Job> lease();
    void ack(int id);
    void release(int id);
};
class Worker {
    Queue& queue_;
public:
    explicit Worker(Queue& q):queue_(q){}
    bool run_one(const std::function<void(const Job&)>& handler);
};
