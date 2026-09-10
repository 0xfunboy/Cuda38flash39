#include "policy.hpp"
#include "services/service_000.hpp"
std::vector<Policy> registry() {
    return {
        service_000::policy_011(),
    };
}
