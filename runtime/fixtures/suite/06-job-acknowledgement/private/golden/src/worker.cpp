#include "jobs.hpp"
bool Worker::run_one(const std::function<void(const Job&)>& handler) {
    auto job=queue_.lease();if(!job)return false;
    try {handler(*job);} catch (...) {queue_.release(job->id);throw;}
    queue_.ack(job->id);
    return true;
}
