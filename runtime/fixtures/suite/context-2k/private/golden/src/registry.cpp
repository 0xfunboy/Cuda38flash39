#include "policy.hpp"
#include "services/service_000.hpp"
#include "services/service_001.hpp"
#include "services/service_002.hpp"
std::vector<Policy> registry() {
    return {
        service_000::policy_011(),
        service_001::policy_048(),
        service_002::policy_085(),
    };
}
