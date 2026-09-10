#include "jobs.hpp"
#include <stdexcept>
void Queue::add(Job job){for(auto& x:items_)if(x.job.id==job.id)throw std::logic_error("duplicate");items_.push_back({std::move(job),State::pending});}
std::optional<Job> Queue::lease(){for(auto& x:items_)if(x.state==State::pending){x.state=State::inflight;return x.job;}return std::nullopt;}
void Queue::ack(int id){for(auto& x:items_)if(x.job.id==id&&x.state==State::inflight){x.state=State::done;return;}throw std::logic_error("not in flight");}
void Queue::release(int id){for(auto& x:items_)if(x.job.id==id&&x.state==State::inflight){x.state=State::pending;return;}throw std::logic_error("not in flight");}
